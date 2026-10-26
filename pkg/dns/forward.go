package dns

import (
	"context"
	"net"
	"time"

	"github.com/pkg/errors"

	"github.com/outofforest/parallel"
)

const numOfForwarders = 10

type forwardRequest struct {
	ForwardIPIndex uint64
	Query          []byte
	ResponseF      responseFunc
	DoneCh         chan struct{}
}

func runForwarders(ctx context.Context, forwardIPs []net.IP, ch <-chan forwardRequest) error {
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for range numOfForwarders {
			spawn("forwarder", parallel.Fail, func(ctx context.Context) error {
				b := make([]byte, udpBufferSize)
				conns := make([]*net.UDPConn, 0, len(forwardIPs))
				defer func() {
					for _, conn := range conns {
						_ = conn.Close()
					}
				}()

				for _, ip := range forwardIPs {
					conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{
						IP:   ip,
						Port: 53,
					})
					if err != nil {
						return errors.WithStack(err)
					}
					conns = append(conns, conn)
				}

				for req := range ch {
					conn := conns[req.ForwardIPIndex]

					if _, err := conn.Write(req.Query); err != nil {
						continue
					}

					if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
						continue
					}
					n, err := conn.Read(b)
					if err != nil {
						continue
					}

					select {
					case <-req.DoneCh:
						if n < headerSize {
							continue
						}
						// Reset AA flag.
						b[2] &= 0xfb
						if err := req.ResponseF(b[:n]); err != nil {
							continue
						}
					default:
					}
				}

				return errors.WithStack(ctx.Err())
			})
		}

		return nil
	})
}
