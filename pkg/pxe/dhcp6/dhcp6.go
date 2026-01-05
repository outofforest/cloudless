package dhcp6

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/net/ipv6"

	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

// tcpdump -i enp9s0 -n -vv '(udp port 546 or 547) or icmp6'

const (
	// Port is the port dhcp6 server listens on.
	Port = 547

	dhcpMulticastGroup = "ff02::1:2"
	bufferSize         = 4096
)

// Run runs DHCP IPv6 server giving random IP addresses required to send EFI payload later.
func Run(ctx context.Context) error {
	for {
		if err := runServer(ctx); err != nil {
			if errors.Is(err, ctx.Err()) {
				return err
			}
			logger.Get(ctx).Error("DHCP server failed", zap.Error(err))
		}
	}
}

func runServer(ctx context.Context) error {
	conn, scopes, err := newListener()
	if err != nil {
		return errors.WithStack(err)
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("watchdog", parallel.Fail, func(ctx context.Context) error {
			<-ctx.Done()
			_ = conn.Close()
			return errors.WithStack(ctx.Err())
		})
		spawn("server", parallel.Fail, func(ctx context.Context) error {
			defer conn.Close()

			log := logger.Get(ctx)

			serverDUID, err := newDUID()
			if err != nil {
				return err
			}

			b := make([]byte, bufferSize)
			iaadrOption := newIAADDROption()

		loop:
			for {
				n, addr, err := conn.ReadFrom(b)
				if err != nil {
					if ctx.Err() != nil {
						return errors.WithStack(ctx.Err())
					}
					return errors.WithStack(err)
				}

				udpAddr := addr.(*net.UDPAddr)
				msg := parseMessage(b[:n])

				switch msg.MessageType {
				case messageTypeSolicit:
					var clientDUID, iaIAID []byte
					for _, o := range msg.Options {
						switch o.OptionCode {
						case optionCodeClientID:
							if clientDUID != nil {
								log.Debug("Duplicated client DUID")
								continue loop
							}
							clientDUID = make([]byte, len(o.OptionData))
							copy(clientDUID, o.OptionData)
						case optionCodeServerID:
							log.Debug("Unexpected server DUID")
							continue loop
						case optionIANA:
							iaIAID = make([]byte, 4)
							copy(iaIAID, o.OptionData[:4])
						}
					}
					if clientDUID == nil {
						log.Debug("Missing client DUID")
						continue loop
					}
					if iaIAID == nil {
						log.Debug("Missing IAID")
						continue loop
					}

					if _, err := conn.WriteTo(serializeMessage(message{
						MessageType:   messageTypeAdvertise,
						TransactionID: msg.TransactionID,
						Options: []option{
							{
								OptionCode: optionCodeClientID,
								OptionData: clientDUID,
							},
							{
								OptionCode: optionCodeServerID,
								OptionData: serverDUID,
							},
							{
								OptionCode: optionBootfileURL,
								OptionData: bootloaderURL(scopes[udpAddr.Zone]),
							},
							{
								OptionCode: optionIANA,
								OptionData: fillIAADDROption(iaadrOption, iaIAID),
							},
						},
					}, b), addr); err != nil {
						return errors.WithStack(err)
					}
				case messageTypeRequest:
					var clientDUID, receivedServerDUID, iaIAID []byte
					for _, o := range msg.Options {
						switch o.OptionCode {
						case optionCodeClientID:
							if clientDUID != nil {
								log.Debug("Duplicated client DUID")
								continue loop
							}
							clientDUID = make([]byte, len(o.OptionData))
							copy(clientDUID, o.OptionData)
						case optionCodeServerID:
							if receivedServerDUID != nil {
								log.Debug("Duplicated server DUID")
								continue loop
							}
							receivedServerDUID = make([]byte, len(o.OptionData))
							copy(receivedServerDUID, o.OptionData)
						case optionIANA:
							iaIAID = make([]byte, 4)
							copy(iaIAID, o.OptionData[:4])
						}
					}
					if clientDUID == nil {
						log.Debug("Missing client DUID")
						continue loop
					}
					if receivedServerDUID == nil {
						log.Debug("Missing server DUID")
						continue loop
					}
					if !bytes.Equal(receivedServerDUID, serverDUID) {
						log.Debug("Server DUID does not match")
						continue loop
					}

					if _, err := conn.WriteTo(serializeMessage(message{
						MessageType:   messageTypeReply,
						TransactionID: msg.TransactionID,
						Options: []option{
							{
								OptionCode: optionCodeClientID,
								OptionData: clientDUID,
							},
							{
								OptionCode: optionCodeServerID,
								OptionData: serverDUID,
							},
							{
								OptionCode: optionBootfileURL,
								OptionData: bootloaderURL(scopes[udpAddr.Zone]),
							},
							{
								OptionCode: optionIANA,
								OptionData: fillIAADDROption(iaadrOption, iaIAID),
							},
						},
					}, b), addr); err != nil {
						return errors.WithStack(err)
					}
				}
			}
		})

		return nil
	})
}

func parseMessage(b []byte) message {
	if len(b) < 4 {
		return message{}
	}
	m := message{
		MessageType:   messageType(b[0]),
		TransactionID: transactionID(b[1:4]),
	}

	b = b[4:]
	for len(b) > 0 {
		if len(b) < 4 {
			return message{}
		}
		optionLen := binary.BigEndian.Uint16(b[2:4])
		if uint16(len(b)) < 4+optionLen {
			return message{}
		}
		m.Options = append(m.Options, option{
			OptionCode: optionCode(binary.BigEndian.Uint16(b[:2])),
			OptionData: b[4 : 4+optionLen],
		})
		b = b[4+optionLen:]
	}

	return m
}

func serializeMessage(m message, b []byte) []byte {
	b = b[:0]
	b = append(b, byte(m.MessageType))
	b = append(b, m.TransactionID[:]...)
	for _, o := range m.Options {
		b = binary.BigEndian.AppendUint16(b, uint16(o.OptionCode))
		b = binary.BigEndian.AppendUint16(b, uint16(len(o.OptionData)))
		b = append(b, o.OptionData...)
	}

	return b
}

func newDUID() ([]byte, error) {
	// https://datatracker.ietf.org/doc/html/rfc8415#section-11.5
	const duidType uint16 = 4

	uuidValue, err := uuid.NewUUID()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	duid := make([]byte, 18)
	binary.BigEndian.PutUint16(duid, duidType)
	copy(duid[2:], uuidValue[:])

	return duid, nil
}

func newListener() (*net.UDPConn, map[string]*net.IPNet, error) {
	conn, err := net.ListenUDP("udp6", &net.UDPAddr{
		IP:   net.IPv6zero,
		Port: Port,
	})
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	scopes := map[string]*net.IPNet{}
	pConn := ipv6.NewPacketConn(conn)
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, nil, errors.WithStack(err)
		}
		for _, addr := range addrs {
			ipAddr, ok := addr.(*net.IPNet)
			if !ok || ipAddr.IP.IsLoopback() || !ipAddr.IP.IsLinkLocalUnicast() {
				continue
			}
			_, maskSize := ipAddr.Mask.Size()
			if maskSize != 128 {
				continue
			}

			if err := pConn.JoinGroup(&iface, &net.UDPAddr{
				IP:   net.ParseIP(dhcpMulticastGroup),
				Port: Port,
			}); err != nil {
				return nil, nil, errors.WithStack(err)
			}

			scopes[iface.Name] = ipAddr

			break
		}
	}

	return conn, scopes, nil
}

func newIAADDROption() []byte {
	// https://datatracker.ietf.org/doc/html/rfc8415#section-21.6

	ianaData := make([]byte, 0, 40)

	// Will be filled later with data received from the client.
	ianaData = append(ianaData, 0x00, 0x00, 0x00, 0x00)
	ianaData = binary.BigEndian.AppendUint32(ianaData, 0)
	ianaData = binary.BigEndian.AppendUint32(ianaData, 0)
	ianaData = binary.BigEndian.AppendUint16(ianaData, 0x05)
	ianaData = binary.BigEndian.AppendUint16(ianaData, 24)
	// Will be filled later with data received from the client.
	ianaData = append(ianaData, net.IPv6zero...)
	ianaData = binary.BigEndian.AppendUint32(ianaData, 600)
	ianaData = binary.BigEndian.AppendUint32(ianaData, 600)

	return ianaData
}

func fillIAADDROption(iaadrOption, iaID []byte) []byte {
	copy(iaadrOption, iaID)
	copy(iaadrOption[16:], selectIP())
	return iaadrOption
}

func selectIP() net.IP {
	// It is assumed that the DHCP network is always fe80::/10 (local-link).

	ip := make(net.IP, 16)
	if _, err := rand.Read(ip); err != nil {
		panic(err)
	}

	// Ensure fe80::/10.
	// Bytes in net.IP are stored in reverted order.
	ip[0] = 0xfe
	ip[1] |= 0x80
	ip[1] &= 0xbf

	// It is assumed that DHCP servers have this bit set to 0.
	// So we set it to 1 to ensure there are no conflicts.
	ip[1] |= 0x20

	return ip
}

func bootloaderURL(baseIP *net.IPNet) []byte {
	return fmt.Appendf([]byte{}, "tftp://[%s]/bootx64.efi", baseIP.IP)
}

type message struct {
	MessageType   messageType
	TransactionID transactionID
	Options       []option
}

type messageType byte

const (
	messageTypeSolicit   messageType = 1
	messageTypeAdvertise messageType = 2
	messageTypeRequest   messageType = 3
	messageTypeReply     messageType = 7
)

type transactionID [3]byte

type option struct {
	OptionCode optionCode
	OptionData []byte
}

type optionCode uint16

const (
	optionCodeClientID optionCode = 1
	optionCodeServerID optionCode = 2
	optionIANA         optionCode = 3
	optionBootfileURL  optionCode = 59
)
