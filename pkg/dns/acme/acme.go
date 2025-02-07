package acme

import (
	"context"
	"net"
	"sync"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/outofforest/cloudless/pkg/dns/acme/wire"
	"github.com/outofforest/resonance"
)

const (
	// Port is the port ACME server listens on.
	Port = 80

	// DomainPrefix is the domain prefix defined by ACME.
	DomainPrefix = "_acme-challenge."
)

// WireConfig is the DNS ACME service wire config.
var WireConfig = resonance.Config[wire.Marshaller]{
	MaxMessageSize:    4 * 1024,
	MarshallerFactory: wire.NewMarshaller,
}

// NewServer creates new ACME server.
func NewServer(port uint16) *Server {
	return &Server{
		port:       port,
		challenges: map[string]map[uuid.UUID]string{},
	}
}

// Server is the ACME server accepting DNS challenges.
type Server struct {
	port       uint16
	mu         sync.Mutex
	challenges map[string]map[uuid.UUID]string
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
				msg, ok := <-recvCh
				if !ok {
					return nil
				}

				req, ok := msg.(*wire.MsgRequest)
				if !ok {
					return errors.New("unrecognized message received")
				}

				id := uuid.New()
				s.storeChallenges(id, req.Challenges)
				defer s.removeChallenges(id, req.Challenges)

				c.Send(&wire.MsgAck{})
			}
		},
	)
}

// Query returns challenges for domain.
func (s *Server) Query(domain string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	d := s.challenges[domain]
	if len(d) == 0 {
		return nil
	}

	values := make([]string, 0, len(d))
	for _, v := range d {
		values = append(values, v)
	}

	return values
}

func (s *Server) storeChallenges(id uuid.UUID, challenges []wire.Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ch := range challenges {
		chs := s.challenges[ch.Domain]
		if chs == nil {
			chs = map[uuid.UUID]string{}
			s.challenges[ch.Domain] = chs
		}
		chs[id] = ch.Value
	}
}

func (s *Server) removeChallenges(id uuid.UUID, challenges []wire.Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ch := range challenges {
		chs := s.challenges[ch.Domain]
		if chs == nil {
			continue
		}
		delete(chs, id)
		if len(chs) == 0 {
			delete(s.challenges, ch.Domain)
		}
	}
}
