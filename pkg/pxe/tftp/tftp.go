package tftp

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"net"
	"os"
	"strconv"

	"github.com/cespare/xxhash"
	"github.com/diskfs/go-diskfs"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

const (
	// Port is the port tftp server listens on.
	Port = 69

	bufferSize        = 4096
	defaultBlockSize  = 512
	maxBlockSize      = bufferSize - 4
	defaultWindowSize = 1
)

// NewRun returns Run function of TFTP server.
func NewRun(efiDevPath string) parallel.Task {
	return func(ctx context.Context) error {
		efiData, err := readBootx64EFI(efiDevPath)
		if err != nil {
			return errors.WithStack(err)
		}

		for {
			if err := runServer(ctx, efiData); err != nil {
				if errors.Is(err, ctx.Err()) {
					return err
				}
				logger.Get(ctx).Error("TFTP server failed", zap.Error(err))
			}
		}
	}
}

func runServer(ctx context.Context, efiData []byte) error {
	conn, err := newListener()
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
			b := make([]byte, bufferSize)

			var blockSize uint64 = defaultBlockSize
			var windowSize uint64 = defaultWindowSize
			efiDataLen := uint64(len(efiData))

			rollOver1 := map[uint64]*rollOver{}
			rollOver2 := map[uint64]*rollOver{}

		loop:
			for {
				n, addr, err := conn.ReadFrom(b)
				if err != nil {
					if ctx.Err() != nil {
						return errors.WithStack(ctx.Err())
					}
					return errors.WithStack(err)
				}

				if n < 2 {
					continue loop
				}

				opCode := opCode(binary.BigEndian.Uint16(b[:2]))

				switch opCode {
				case rrqOpCode:
					rrq, err := parseReadRequest(b[:n])
					if err != nil {
						log.Error("Parsing RRQ failed.", zap.Error(err))
						continue loop
					}

					if rrq.Filename != "bootx64.efi" {
						continue loop
					}

					clearRollOver(addr.(*net.UDPAddr).IP, rollOver1, rollOver2)

					if len(rrq.Options) == 0 {
						end := min(blockSize, efiDataLen)
						if _, err := conn.WriteTo(prepareDataMessage(1, efiData[:end], b), addr); err != nil {
							return errors.WithStack(err)
						}

						continue
					}

					options := make([]option, 0, len(rrq.Options))
					for _, opt := range rrq.Options {
						switch opt.Name {
						case "tsize":
							options = append(options, option{
								Name:  opt.Name,
								Value: strconv.Itoa(int(efiDataLen)),
							})
						case "blksize":
							var err error
							blockSize, err = strconv.ParseUint(opt.Value, 10, 64)
							if err != nil {
								log.Error("Parsing block size failed.", zap.Error(err))
								continue loop
							}
							if blockSize > maxBlockSize {
								blockSize = maxBlockSize
							}
							options = append(options, opt)
						case "windowSize":
							var err error
							windowSize, err = strconv.ParseUint(opt.Value, 10, 64)
							if err != nil {
								log.Error("Parsing window size failed.", zap.Error(err))
								continue loop
							}
							options = append(options, opt)
						}
					}

					if _, err := conn.WriteTo(prepareOAckMessage(options, b), addr); err != nil {
						return errors.WithStack(err)
					}
				case ackOpCode:
					if n < 4 {
						continue loop
					}
					block := uint64(binary.BigEndian.Uint16(b[2:4]))
					ro := updateRollOver(addr.(*net.UDPAddr).IP, block, &rollOver1, &rollOver2)

					for i := range windowSize {
						start := (ro*(math.MaxUint16+uint64(1)) + block + i) * blockSize
						if start > efiDataLen {
							clearRollOver(addr.(*net.UDPAddr).IP, rollOver1, rollOver2)
							continue loop
						}
						end := min(start+blockSize, efiDataLen)
						if _, err := conn.WriteTo(prepareDataMessage(block+i+1, efiData[start:end], b), addr); err != nil {
							return errors.WithStack(err)
						}
					}
				}
			}
		})

		return nil
	})
}

func newListener() (*net.UDPConn, error) {
	conn, err := net.ListenUDP("udp6", &net.UDPAddr{
		IP:   net.IPv6zero,
		Port: Port,
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return conn, nil
}

func prepareDataMessage(block uint64, data []byte, b []byte) []byte {
	b = b[:0]
	b = binary.BigEndian.AppendUint16(b, uint16(dataOpCode))
	b = binary.BigEndian.AppendUint16(b, uint16(block))
	b = append(b, data...)
	return b
}

func prepareOAckMessage(options []option, b []byte) []byte {
	b = b[:0]
	b = binary.BigEndian.AppendUint16(b, uint16(oACKOpCode))
	for _, opt := range options {
		b = append(b, []byte(opt.Name)...)
		b = append(b, 0x00)
		b = append(b, []byte(opt.Value)...)
		b = append(b, 0x00)
	}
	return b
}

func parseReadRequest(b []byte) (readRequest, error) {
	var r readRequest

	b = b[2:]
	filename, b, err := readString(b)
	if err != nil {
		return readRequest{}, err
	}
	r.Filename = filename

	// mode
	_, b, err = readString(b)
	if err != nil {
		return readRequest{}, err
	}

	for len(b) > 0 {
		var err error
		var opt option
		opt.Name, b, err = readString(b)
		if err != nil {
			return readRequest{}, err
		}
		opt.Value, b, err = readString(b)
		if err != nil {
			return readRequest{}, err
		}

		r.Options = append(r.Options, opt)
	}

	return r, nil
}

func readString(b []byte) (string, []byte, error) {
	for i, v := range b {
		if v == 0x00 {
			return string(b[:i]), b[i+1:], nil
		}
	}

	return "", nil, errors.New("no string termination found")
}

func readBootx64EFI(efiDevPath string) ([]byte, error) {
	disk, err := diskfs.Open(efiDevPath, diskfs.WithOpenMode(diskfs.ReadOnly))
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer disk.Close()

	fs, err := disk.GetFilesystem(0)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	f, err := fs.OpenFile("/EFI/BOOT/bootx64.efi", os.O_RDONLY)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return data, err
}

func clearRollOver(ip net.IP, rollOver1, rollOver2 map[uint64]*rollOver) {
	hash := xxhash.Sum64(ip)
	delete(rollOver1, hash)
	delete(rollOver2, hash)
}

func updateRollOver(ip net.IP, ackBlock uint64, rollOver1, rollOver2 *map[uint64]*rollOver) uint64 {
	const maxRecords = 1000

	hash := xxhash.Sum64(ip)
	ro := (*rollOver2)[hash]
	if ro == nil {
		ro = (*rollOver1)[hash]
		if len(*rollOver2) == maxRecords-1 {
			*rollOver1, *rollOver2 = *rollOver2, *rollOver1
			clear(*rollOver2)
		}
	}
	if ro == nil {
		ro = &rollOver{}
		(*rollOver2)[hash] = ro
	}

	if uint16(ackBlock) < ro.LastACK {
		ro.RollOver++
	}
	ro.LastACK = uint16(ackBlock)

	return uint64(ro.RollOver)
}

type opCode uint16

const (
	rrqOpCode  opCode = 1
	dataOpCode opCode = 3
	ackOpCode  opCode = 4
	oACKOpCode opCode = 6
)

type readRequest struct {
	Filename string
	Options  []option
}

type option struct {
	Name  string
	Value string
}

type rollOver struct {
	LastACK  uint16
	RollOver uint16
}
