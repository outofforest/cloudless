package dkim

import (
	"context"
	"encoding/base64"
	"net"
	"sync"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless/pkg/dns/dkim/wire"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/resonance"
)

const (
	// Port is the port DKIM server listens on.
	Port = 81

	// DomainPrefix is the domain prefix defined by ACME.
	DomainPrefix = "._domainkey."
)

// WireConfig is the DNS ACME service wire config.
var WireConfig = resonance.Config[wire.Marshaller]{
	MaxMessageSize:    4 * 1024,
	MarshallerFactory: wire.NewMarshaller,
}

// Address returns address of dns acme endpoint.
func Address(host string) string {
	return tnet.Join(host, Port)
}

// NewServer creates new DKIM server.
func NewServer(port uint16) *Server {
	return &Server{
		port:       port,
		publicKeys: map[string]string{},
	}
}

// Server is the DKIM server accepting DNS record requests.
type Server struct {
	port uint16

	mu         sync.Mutex
	publicKeys map[string]string
}

// Run runs ACME server.
func (s *Server) Run(ctx context.Context) error {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.IPv4zero,
		Port: int(s.port),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	return resonance.RunServer(ctx, l, WireConfig,
		func(ctx context.Context, recvCh <-chan any, c *resonance.Connection[wire.Marshaller]) error {
			for {
				var msg any
				var ok bool
				select {
				case <-ctx.Done():
					return nil
				case msg, ok = <-recvCh:
					if !ok {
						return nil
					}
				}

				req, ok := msg.(*wire.MsgRequest)
				if !ok {
					return errors.New("unrecognized message received")
				}

				s.storeRequest(req)

				c.Send(&wire.MsgAck{})
			}
		},
	)
}

// PublicKey returns base64-encoded public key for provider.
func (s *Server) PublicKey(provider string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.publicKeys[provider]
}

func (s *Server) storeRequest(req *wire.MsgRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.publicKeys[req.Provider] = base64.StdEncoding.EncodeToString(req.PublicKey)
}
