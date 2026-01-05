package acme

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/outofforest/cloudless/pkg/dns/acme/wire"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/resonance"
)

const (
	// Port is the port ACME server listens on.
	Port = 80

	// DomainPrefix is the domain prefix defined by ACME.
	DomainPrefix = "_acme-challenge."
)

// WireConfig is the DNS ACME service wire config.
var WireConfig = resonance.Config{
	MaxMessageSize: 4 * 1024,
}

// Address returns address of dns acme endpoint.
func Address(host string) string {
	return tnet.Join(host, Port)
}

// Server is the ACME server accepting DNS challenges.
type Server struct {
	port uint16

	mu         sync.Mutex
	challenges map[string]map[uuid.UUID]acmeRecord
}

// NewServer creates new ACME server.
func NewServer(port uint16) *Server {
	return &Server{
		port:       port,
		challenges: map[string]map[uuid.UUID]acmeRecord{},
	}
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

	m := wire.NewMarshaller()
	return resonance.RunServer(ctx, l, WireConfig,
		func(ctx context.Context, c *resonance.Connection) error {
			for {
				msg, err := c.ReceiveProton(m)
				if err != nil {
					return err
				}

				req, ok := msg.(*wire.MsgRequest)
				if !ok {
					return errors.New("unrecognized message received")
				}

				id := uuid.New()
				s.storeRequest(id, req)
				defer s.removeChallenges(id, req.Challenges)

				if err := c.SendProton(&wire.MsgAck{}, m); err != nil {
					return err
				}
			}
		},
	)
}

// QueryTXT returns TXT challenge responses for domain.
func (s *Server) QueryTXT(domain string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	d := s.challenges[domain]
	if len(d) == 0 {
		return nil
	}

	values := make([]string, 0, len(d))
	for _, v := range d {
		values = append(values, v.Value)
	}

	return values
}

// QueryCAA returns CAA responses for domain.
func (s *Server) QueryCAA(domain string) []CAA {
	s.mu.Lock()
	defer s.mu.Unlock()

	d := s.challenges[domain]
	if len(d) == 0 {
		return nil
	}

	values := make([]CAA, 0, len(d))
	for _, v := range d {
		values = append(values, v.CAA)
	}

	return values
}

func (s *Server) storeRequest(id uuid.UUID, req *wire.MsgRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ch := range req.Challenges {
		chs := s.challenges[ch.Domain]
		if chs == nil {
			chs = map[uuid.UUID]acmeRecord{}
			s.challenges[ch.Domain] = chs
		}
		chs[id] = acmeRecord{
			Value: ch.Value,
			CAA: CAA{
				Flags: 128,
				Tag:   "issue",
				Value: fmt.Sprintf("%s;accounturi=%s;validationmethods=dns-01", req.Provider, req.AccountURI),
			},
		}
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

type acmeRecord struct {
	Value string
	CAA   CAA
}

// CAA represents CAA record.
type CAA struct {
	Flags uint8
	Tag   string
	Value string
}
