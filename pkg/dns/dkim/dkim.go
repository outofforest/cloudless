package dkim

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless/pkg/dns/dkim/wire"
	"github.com/outofforest/parallel"
	"github.com/outofforest/wave"
)

const (
	// Port is the port DKIM server listens on.
	Port = 81

	// RefreshInterval is the interval records should be refreshed with.
	RefreshInterval = time.Minute

	domainPrefix = "._domainkey."
)

type record struct {
	TimeAdded time.Time
	PublicKey string
}

// Handler is the DKIM handler accepting DNS record requests.
type Handler struct {
	waveServers []string

	mu      sync.Mutex
	records map[string]record
}

// New creates new DKIM handler.
func New(waveServers []string) *Handler {
	return &Handler{
		waveServers: waveServers,
		records:     map[string]record{},
	}
}

// Run runs DKIM handler.
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
					h.cleanRecords()
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

					h.storeRequest(reqMsg)
				}
			}
		})

		return nil
	})
}

// PublicKey returns base64-encoded public key for provider.
func (h *Handler) PublicKey(query, domain string) string {
	provider := strings.TrimSuffix(query, domainPrefix+domain)

	h.mu.Lock()
	defer h.mu.Unlock()

	return h.records[provider].PublicKey
}

func (h *Handler) storeRequest(req *wire.MsgRequest) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.records[req.Provider] = record{
		TimeAdded: time.Now(),
		PublicKey: base64.StdEncoding.EncodeToString(req.PublicKey),
	}
}

func (h *Handler) cleanRecords() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for p, r := range h.records {
		if time.Since(r.TimeAdded) > 5*RefreshInterval {
			delete(h.records, p)
		}
	}
}

// IsDKIMQuery checks if query is related to DKIM.
func IsDKIMQuery(query, domain string) bool {
	return strings.HasSuffix(query, domainPrefix+domain)
}

// Domain returns DKIM domain for provider and domain.
func Domain(provider, domain string) string {
	return provider + domainPrefix + domain
}
