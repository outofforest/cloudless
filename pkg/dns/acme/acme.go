package acme

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless/pkg/dns/acme/wire"
	"github.com/outofforest/parallel"
	"github.com/outofforest/wave"
)

const (
	// DomainPrefix is the domain prefix defined by ACME.
	DomainPrefix = "_acme-challenge."
)

// Handler is the ACME handler resolving challenges.
type Handler struct {
	waveServers []string

	mu         sync.Mutex
	challenges map[string]map[[32]byte]acmeRecord
}

// New creates new ACME handler.
func New(waveServers []string) *Handler {
	return &Handler{
		waveServers: waveServers,
		challenges:  map[string]map[[32]byte]acmeRecord{},
	}
}

// Run runs ACME handler.
func (h *Handler) Run(ctx context.Context) error {
	m := wire.NewMarshaller()

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		waveClient, waveCh, err := wave.NewClient(wave.ClientConfig{
			Servers:        h.waveServers,
			MaxMessageSize: 1024,
			Requests: []wave.RequestConfig{
				{
					Marshaller: m,
					Messages:   []any{&wire.MsgRequest{}},
				},
			},
		})
		if err != nil {
			return err
		}

		spawn("wave", parallel.Fail, waveClient.Run)
		spawn("clean", parallel.Fail, func(ctx context.Context) error {
			for {
				select {
				case <-ctx.Done():
					return errors.WithStack(ctx.Err())
				case <-time.After(time.Minute):
					h.cleanChallenges()
				}
			}
		})
		spawn("receiver", parallel.Fail, func(ctx context.Context) error {
			for {
				select {
				case <-ctx.Done():
					return errors.WithStack(ctx.Err())
				case msg := <-waveCh:
					reqMsg, ok := msg.(*wire.MsgRequest)
					if !ok {
						return errors.New("unexpected message type")
					}

					if err := h.storeRequest(reqMsg); err != nil {
						return err
					}
				}
			}
		})

		return nil
	})
}

// QueryTXT returns TXT challenge responses for domain.
func (h *Handler) QueryTXT(domain string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	d := h.challenges[domain]
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
func (h *Handler) QueryCAA(domain string) []CAA {
	h.mu.Lock()
	defer h.mu.Unlock()

	d := h.challenges[domain]
	if len(d) == 0 {
		return nil
	}

	values := make([]CAA, 0, len(d))
	for _, v := range d {
		values = append(values, v.CAA)
	}

	return values
}

func (h *Handler) storeRequest(req *wire.MsgRequest) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var id [32]byte
	t := time.Now()

	_, err := rand.Read(id[:])
	if err != nil {
		return errors.WithStack(err)
	}

	for _, ch := range req.Challenges {
		chs := h.challenges[ch.Domain]
		if chs == nil {
			chs = map[[32]byte]acmeRecord{}
			h.challenges[ch.Domain] = chs
		}
		chs[id] = acmeRecord{
			TimeAdded: t,
			Value:     ch.Value,
			CAA: CAA{
				Flags: 128,
				Tag:   "issue",
				Value: fmt.Sprintf("%s;accounturi=%s;validationmethods=dns-01", req.Provider, req.AccountURI),
			},
		}
	}

	return nil
}

func (h *Handler) cleanChallenges() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for d, chs := range h.challenges {
		for id, r := range chs {
			if time.Since(r.TimeAdded) > time.Minute {
				delete(chs, id)
			}
		}
		if len(chs) == 0 {
			delete(h.challenges, d)
		}
	}
}

type acmeRecord struct {
	TimeAdded time.Time
	Value     string
	CAA       CAA
}

// CAA represents CAA record.
type CAA struct {
	Flags uint8
	Tag   string
	Value string
}
