package dns

import (
	"context"
	"encoding/binary"
	"net"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/net/ipv4"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/dns/acme"
	"github.com/outofforest/cloudless/pkg/dns/dkim"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/logger"
	"github.com/outofforest/mass"
	"github.com/outofforest/parallel"
)

const (
	// Port is the port DNS listens on.
	Port = 53

	bufferSize          = 1500
	defaultMaxMsgLength = 512
	headerSize          = 12
	ttl                 = 60
	forwardChCapacity   = 10

	classInternet = 1

	typeA     = 1
	typeNS    = 2
	typeSOA   = 6
	typeCNAME = 5
	typeMX    = 15
	typeTXT   = 16
	typeOPT   = 41
	typeCAA   = 257

	rCodeOK             = 0
	rCodeFormatError    = 1
	rCodeServerFailure  = 2
	rCodeNameError      = 3
	rCodeNotImplemented = 4
	rCodeRefused        = 5
)

// Service returns DNS service.
func Service(configurators ...Configurator) host.Configurator {
	config := Config{
		DNSPort:  Port,
		ACMEPort: acme.Port,
		DKIMPort: dkim.Port,
		Zones:    map[string]ZoneConfig{},
	}
	for _, configurator := range configurators {
		configurator(&config)
	}

	return cloudless.Service("dns", parallel.Fail, func(ctx context.Context) error {
		return run(ctx, config)
	})
}

func run(ctx context.Context, config Config) error {
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		var acmeServer *acme.Server
		var dkimServer *dkim.Server
		var forwardCh chan forwardRequest

		if config.EnableACME {
			acmeServer = acme.NewServer(config.ACMEPort)
			spawn("acme", parallel.Fail, acmeServer.Run)
		}
		if config.EnableACME {
			dkimServer = dkim.NewServer(config.DKIMPort)
			spawn("dkim", parallel.Fail, dkimServer.Run)
		}
		if len(config.ForwardFor) > 0 && len(config.ForwardTo) > 0 {
			forwardCh = make(chan forwardRequest, forwardChCapacity)

			spawn("forwarder", parallel.Fail, func(ctx context.Context) error {
				return runForwarders(ctx, config.ForwardTo, forwardCh)
			})
		}
		spawn("resolver", parallel.Fail, func(ctx context.Context) error {
			if forwardCh != nil {
				defer close(forwardCh)
			}

			for {
				if err := runResolver(ctx, config, forwardCh, acmeServer, dkimServer); err != nil {
					if errors.Is(err, ctx.Err()) {
						return err
					}
					logger.Get(ctx).Error("DNS server failed", zap.Error(err))
				}
			}
		})

		return nil
	})
}

//nolint:gocyclo
func runResolver(
	ctx context.Context,
	config Config,
	forwardCh chan<- forwardRequest,
	acmeServer *acme.Server,
	dkimServer *dkim.Server,
) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: int(config.DNSPort),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	if err := ipv4.NewPacketConn(conn).SetControlMessage(ipv4.FlagDst|ipv4.FlagInterface, true); err != nil {
		return errors.WithStack(err)
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("watchdog", parallel.Fail, func(ctx context.Context) error {
			<-ctx.Done()
			_ = conn.Close()
			return errors.WithStack(ctx.Err())
		})
		spawn("server", parallel.Fail, func(ctx context.Context) error {
			buff := make([]byte, bufferSize)
			ooBuff := make([]byte, bufferSize)
			massBuff := mass.New[byte](100 * bufferSize)

			var queryID uint64
			cm := &ipv4.ControlMessage{}

		loop:
			for {
				n, noob, _, addr, err := conn.ReadMsgUDP(buff, ooBuff)
				if err != nil {
					if ctx.Err() != nil {
						return errors.WithStack(ctx.Err())
					}
					return errors.WithStack(err)
				}
				if err := cm.Parse(ooBuff[:noob]); err != nil {
					return errors.WithStack(err)
				}
				cm.Src, cm.Dst = cm.Dst, nil

				h, ok := readHeader(buff[:n])
				if !ok || h.QR || h.TC || h.QDCount == 0 || h.RCode != 0x00 || h.ANCount != 0 || h.NSCount != 0 {
					h.RCode = rCodeFormatError
					if err := sendError(h, addr, conn, buff, cm); err != nil {
						return err
					}
					continue loop
				}
				if h.Opcode != 0x00 || h.QDCount > 1 {
					h.RCode = rCodeNotImplemented
					if err := sendError(h, addr, conn, buff, cm); err != nil {
						return err
					}
					continue loop
				}

				arCount := h.ARCount

				h.QR = true
				h.AA = true
				h.RA = h.RD && forwardCh != nil && forwardingAllowed(addr, config.ForwardFor)
				h.QDCount = 0
				h.ANCount = 0
				h.NSCount = 0
				h.ARCount = 0

				q, b, ok := readQuery(buff[headerSize:n])
				if !ok || q.QName == "" {
					h.RCode = rCodeFormatError
					if err := sendError(h, addr, conn, buff, cm); err != nil {
						return err
					}
					continue loop
				}

				var maxMsgLength uint16 = defaultMaxMsgLength

				var opt *rRecord
				for range arCount {
					var r rRecord
					var ok bool

					r, b, ok = readRecord(b)
					if !ok || uint16(len(b)) < r.RDLength {
						h.RCode = rCodeFormatError
						if err := sendError(h, addr, conn, buff, cm); err != nil {
							return err
						}
						continue loop
					}
					b = b[r.RDLength:]

					if r.Type != typeOPT {
						continue
					}

					if opt != nil || r.Name != "" || r.Class < defaultMaxMsgLength {
						h.RCode = rCodeFormatError
						if err := sendError(h, addr, conn, buff, cm); err != nil {
							return err
						}
						continue loop
					}

					opt = &r

					opt.TTL = 0
					opt.RDLength = 0

					if opt.Class > bufferSize {
						opt.Class = bufferSize
					}

					maxMsgLength = opt.Class
				}

				b = putQuery(q, buff[headerSize:headerSize], &h, maxMsgLength)

				if q.QClass != classInternet {
					h.RCode = rCodeNotImplemented
					if err := sendError(h, addr, conn, b, cm); err != nil {
						return err
					}
					continue loop
				}

				q.QName = strings.ToLower(q.QName)
				zConfig, ok := zone(q.QName, config.Zones)
				//nolint:nestif
				if !ok {
					if h.RA {
						fBuff := massBuff.NewSlice(uint64(n))
						copy(fBuff, buff)
						doneCh := make(chan struct{}, 1)
						doneCh <- struct{}{}

						var successCount uint64
						for fi := range uint64(len(config.ForwardTo)) {
							select {
							case forwardCh <- forwardRequest{
								ForwardIPIndex: fi,
								Query:          fBuff,
								Conn:           conn,
								Address:        addr,
								DoneCh:         doneCh,
							}:
								successCount++
							default:
							}
						}
						if successCount == 0 {
							h.RCode = rCodeServerFailure
							if err := sendError(h, addr, conn, b, cm); err != nil {
								return err
							}
						}
						continue loop
					}

					h.RCode = rCodeRefused
					if err := sendError(h, addr, conn, b, cm); err != nil {
						return err
					}

					continue loop
				}

				queryID++
				b = resolve(q, zConfig, b, queryID, acmeServer, dkimServer, &h, maxMsgLength)

				if opt != nil {
					b = putRecord(*opt, b, &h, maxMsgLength)
				}

				putHeader(h, buff[:0])

				if h.RCode != rCodeOK {
					b = nil
				}

				if _, _, err := conn.WriteMsgUDP(buff[:headerSize+len(b)], cm.Marshal(), addr); err != nil {
					return err
				}
			}
		})

		return nil
	})
}

func forwardingAllowed(addr *net.UDPAddr, forwardFor []net.IPNet) bool {
	for _, n := range forwardFor {
		if n.Contains(addr.IP) {
			return true
		}
	}
	return false
}

func sendError(h header, addr *net.UDPAddr, conn *net.UDPConn, b []byte, cm *ipv4.ControlMessage) error {
	h.QR = true
	h.AA = true
	h.TC = false
	h.RA = false
	h.QDCount = 0
	h.ANCount = 0
	h.NSCount = 0
	h.ARCount = 0

	putHeader(h, b[:0])
	_, _, err := conn.WriteMsgUDP(b[:headerSize], cm.Marshal(), addr)
	return errors.WithStack(err)
}

//nolint:gocyclo
func resolve(
	q query,
	zConfig ZoneConfig,
	b []byte,
	queryID uint64,
	acmeServer *acme.Server,
	dkimServer *dkim.Server,
	h *header,
	maxMsgLength uint16,
) []byte {
	switch q.QType {
	case typeSOA:
		if q.QName != zConfig.Domain {
			h.RCode = rCodeNameError
			return b
		}

		email := strings.Replace(zConfig.Email, "@", ".", 1)
		b = putRecord(rRecord{
			Name:     q.QName,
			Type:     typeSOA,
			Class:    classInternet,
			TTL:      ttl,
			RDLength: nameLen(zConfig.MainNameserver) + nameLen(email) + 20,
		}, b, h, maxMsgLength)
		if h.TC {
			return b
		}
		b = putName(zConfig.MainNameserver, b)
		b = putName(email, b)
		b = binary.BigEndian.AppendUint32(b, zConfig.SerialNumber)
		b = binary.BigEndian.AppendUint32(b, ttl)
		b = binary.BigEndian.AppendUint32(b, ttl)
		b = binary.BigEndian.AppendUint32(b, ttl)
		b = binary.BigEndian.AppendUint32(b, ttl)

		return b
	case typeNS:
		if q.QName != zConfig.Domain || len(zConfig.Nameservers) == 0 {
			return b
		}

		for i := range uint64(len(zConfig.Nameservers)) {
			ns := zConfig.Nameservers[(queryID+i)%uint64(len(zConfig.Nameservers))]
			b = putRecord(rRecord{
				Name:     q.QName,
				Type:     typeNS,
				Class:    classInternet,
				TTL:      ttl,
				RDLength: nameLen(ns),
			}, b, h, maxMsgLength)
			if h.TC {
				return b
			}
			b = putName(ns, b)
		}

		return b
	case typeMX:
		if q.QName != zConfig.Domain || len(zConfig.MailExchanges) == 0 {
			return b
		}

		for d, p := range zConfig.MailExchanges {
			b = putRecord(rRecord{
				Name:     q.QName,
				Type:     typeMX,
				Class:    classInternet,
				TTL:      ttl,
				RDLength: 2 + nameLen(d),
			}, b, h, maxMsgLength)
			if h.TC {
				return b
			}
			b = binary.BigEndian.AppendUint16(b, p)
			b = putName(d, b)
		}

		return b
	}

	for {
		alias := zConfig.Aliases[q.QName]
		if alias.Target == "" {
			break
		}

		// cycle protection
		if alias.QueryID == queryID {
			h.RCode = rCodeServerFailure
			return b
		}
		alias.QueryID = queryID
		zConfig.Aliases[q.QName] = alias

		b = putRecord(rRecord{
			Name:     q.QName,
			Type:     typeCNAME,
			Class:    classInternet,
			TTL:      ttl,
			RDLength: nameLen(alias.Target),
		}, b, h, maxMsgLength)
		if h.TC {
			return b
		}
		b = putName(alias.Target, b)

		if q.QType == typeCNAME {
			return b
		}

		q.QName = alias.Target
	}

	switch q.QType {
	case typeCNAME:
	case typeA:
		ips := zConfig.Domains[q.QName]
		for i := range uint64(len(ips)) {
			ip := ips[(queryID+i)%uint64(len(ips))]
			b = putRecord(rRecord{
				Name:     q.QName,
				Type:     typeA,
				Class:    classInternet,
				TTL:      ttl,
				RDLength: 4,
			}, b, h, maxMsgLength)
			if h.TC {
				return b
			}
			b = append(b, ip...)
		}
	case typeTXT:
		values := zConfig.Texts[q.QName]
		if len(values) == 0 {
			switch {
			case acmeServer != nil && strings.HasPrefix(q.QName, acme.DomainPrefix):
				values = acmeServer.QueryTXT(strings.TrimPrefix(q.QName, acme.DomainPrefix))
			case dkimServer != nil && strings.HasSuffix(q.QName, dkim.DomainPrefix+zConfig.Domain):
				publicKey := dkimServer.PublicKey(strings.TrimSuffix(q.QName, dkim.DomainPrefix+zConfig.Domain))
				if publicKey != "" {
					values = []string{"v=DKIM1;k=rsa;p=" + publicKey}
				}
			}
		}
		for _, v := range values {
			iLength := len(v)
			var oLength uint16
			for iLength > 0 {
				if iLength < 256 {
					oLength += uint16(iLength) + 1
					break
				}
				oLength += 256
				iLength -= 255
			}
			b = putRecord(rRecord{
				Name:     q.QName,
				Type:     typeTXT,
				Class:    classInternet,
				TTL:      ttl,
				RDLength: oLength,
			}, b, h, maxMsgLength)
			if h.TC {
				return b
			}
			for len(v) > 0 {
				if len(v) < 256 {
					b = append(b, uint8(len(v)))
					b = append(b, v...)
					break
				}
				b = append(b, uint8(255))
				b = append(b, v[:255]...)
				v = v[255:]
			}
		}
	case typeCAA:
		var values []acme.CAA
		if acmeServer != nil {
			values = acmeServer.QueryCAA(strings.TrimPrefix(q.QName, acme.DomainPrefix))
		}

		// FIXME (wojciech): This causes issues if DNS responds to queries but does not receive ACME challenges.
		if len(values) == 0 {
			values = append(values, []acme.CAA{
				{
					Tag:   "issue",
					Value: ";",
				},
			}...)
		}

		for _, v := range values {
			b = putRecord(rRecord{
				Name:     q.QName,
				Type:     typeCAA,
				Class:    classInternet,
				TTL:      ttl,
				RDLength: 2 + uint16(len(v.Tag)) + uint16(len(v.Value)),
			}, b, h, maxMsgLength)
			if h.TC {
				return b
			}
			b = append(b, v.Flags)
			b = append(b, uint8(len(v.Tag)))
			b = append(b, v.Tag...)
			b = append(b, v.Value...)
		}
	}

	return b
}

func zone(qName string, zones map[string]ZoneConfig) (ZoneConfig, bool) {
	for {
		if zone, ok := zones[qName]; ok {
			return zone, true
		}
		pos := strings.Index(qName, ".")
		if pos < 0 {
			return ZoneConfig{}, false
		}
		qName = qName[pos+1:]
	}
}

func readHeader(b []byte) (header, bool) {
	var h header

	if len(b) < 2 {
		return h, false
	}

	h.ID = binary.BigEndian.Uint16(b)

	if len(b) < headerSize {
		return h, false
	}

	h.QR = b[2]&0x80 != 0x00
	h.Opcode = (b[2] & 0x7f) >> 3
	h.AA = b[2]&0x04 != 0x00
	h.TC = b[2]&0x02 != 0x00
	h.RD = b[2]&0x01 != 0x00
	h.RA = b[3]&0x80 != 0x00
	h.RCode = b[3] & 0x0f
	h.QDCount = binary.BigEndian.Uint16(b[4:])
	h.ANCount = binary.BigEndian.Uint16(b[6:])
	h.NSCount = binary.BigEndian.Uint16(b[8:])
	h.ARCount = binary.BigEndian.Uint16(b[10:])

	return h, true
}

func readQuery(b []byte) (query, []byte, bool) {
	var q query
	var ok bool

	q.QName, b, ok = readName(b)
	if !ok {
		return query{}, nil, false
	}

	if len(b) < 4 {
		return query{}, nil, false
	}

	q.QType = binary.BigEndian.Uint16(b)
	q.QClass = binary.BigEndian.Uint16(b[2:])

	return q, b[4:], true
}

func readRecord(b []byte) (rRecord, []byte, bool) {
	var r rRecord
	var ok bool

	r.Name, b, ok = readName(b)
	if !ok {
		return rRecord{}, nil, false
	}

	if len(b) < 10 {
		return rRecord{}, nil, false
	}

	r.Type = binary.BigEndian.Uint16(b)
	r.Class = binary.BigEndian.Uint16(b[2:])
	r.TTL = binary.BigEndian.Uint32(b[4:])
	r.RDLength = binary.BigEndian.Uint16(b[8:])

	return r, b[10:], true
}

func readName(b []byte) (string, []byte, bool) {
	var name string

	for {
		if len(b) == 0 {
			return "", nil, false
		}

		l := int(b[0])
		if l == 0 {
			return name, b[1:], true
		}
		if len(b) < 1+l {
			return "", nil, false
		}

		if len(name) > 0 {
			name += "."
		}
		name += string(b[1 : 1+l])
		b = b[1+l:]
	}
}

func putHeader(h header, b []byte) {
	b = binary.BigEndian.AppendUint16(b, h.ID)
	b = append(b, h.Opcode<<3)
	if h.QR {
		b[2] |= 0x80
	}
	if h.AA {
		b[2] |= 0x04
	}
	if h.TC {
		b[2] |= 0x02
	}
	if h.RD {
		b[2] |= 0x01
	}
	b = append(b, h.RCode)
	if h.RA {
		b[3] |= 0x80
	}
	b = binary.BigEndian.AppendUint16(b, h.QDCount)
	b = binary.BigEndian.AppendUint16(b, h.ANCount)
	b = binary.BigEndian.AppendUint16(b, h.NSCount)
	binary.BigEndian.AppendUint16(b, h.ARCount)
}

func putQuery(q query, b []byte, h *header, maxMsgLength uint16) []byte {
	length := uint16(len(b)) + nameLen(q.QName) + 4
	if length > maxMsgLength {
		h.TC = true
		return nil
	}

	h.QDCount = 1
	b = putName(q.QName, b)
	b = binary.BigEndian.AppendUint16(b, q.QType)
	return binary.BigEndian.AppendUint16(b, q.QClass)
}

func putRecord(r rRecord, b []byte, h *header, maxMsgLength uint16) []byte {
	length := uint16(len(b)) + nameLen(r.Name) + 10 + r.RDLength + headerSize
	if length > maxMsgLength {
		h.TC = true
		return nil
	}

	if r.Type == typeOPT {
		h.ARCount++
	} else {
		h.ANCount++
	}
	b = putName(r.Name, b)
	b = binary.BigEndian.AppendUint16(b, r.Type)
	b = binary.BigEndian.AppendUint16(b, r.Class)
	b = binary.BigEndian.AppendUint32(b, r.TTL)
	return binary.BigEndian.AppendUint16(b, r.RDLength)
}

func putName(name string, b []byte) []byte {
	if name != "" {
		for _, part := range strings.Split(name, ".") {
			b = append(b, uint8(len(part)))
			b = append(b, part...)
		}
	}
	return append(b, 0x00)
}

func nameLen(name string) uint16 {
	if name == "" {
		return 1
	}
	return uint16(len(name)) + 2
}

type header struct {
	ID      uint16
	QR      bool
	Opcode  uint8
	AA      bool
	TC      bool
	RD      bool
	RA      bool
	RCode   uint8
	QDCount uint16
	ANCount uint16
	NSCount uint16
	ARCount uint16
}

type query struct {
	QName  string
	QType  uint16
	QClass uint16
}

type rRecord struct {
	Name     string
	Type     uint16
	Class    uint16
	TTL      uint32
	RDLength uint16
}
