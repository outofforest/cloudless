package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	goacme "golang.org/x/crypto/acme"

	"github.com/outofforest/cloudless"
	dnsacme "github.com/outofforest/cloudless/pkg/dns/acme"
	"github.com/outofforest/cloudless/pkg/dns/acme/wire"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
	"github.com/outofforest/resonance"
)

const (
	rerunInterval            = 5 * time.Second
	dnsACMEConnectionTimeout = 20 * time.Second
	dnsACMEOnboardTimeout    = 2 * time.Second
	dnsACMEACKTimeout        = 5 * time.Second
)

// Service returns new acme client service.
func Service(dirConfig DirectoryConfig, configurators ...Configurator) host.Configurator {
	return cloudless.Join(
		cloudless.Service("acme", parallel.Fail, func(ctx context.Context) error {
			config := Config{
				Directory: dirConfig,
			}

			for _, configurator := range configurators {
				configurator(&config)
			}

			if len(config.Domains) == 0 {
				return errors.New("no domains defined")
			}
			if len(config.DNSACME) == 0 {
				return errors.New("no dns acme endpoints defined")
			}

			for {
				if err := run(ctx, config); err != nil {
					if errors.Is(err, ctx.Err()) {
						return err
					}
					logger.Get(ctx).Error("ACME failed", zap.Error(err))
				}

				select {
				case <-ctx.Done():
					return errors.WithStack(ctx.Err())
				case <-time.After(rerunInterval):
				}
			}
		}),
	)
}

type challenge struct {
	Domain    string
	AuthZURI  string
	Challenge *goacme.Challenge
}

type order struct {
	OrderURI   string
	Challenges []challenge
}

//nolint:gocyclo
func run(ctx context.Context, config Config) error {
	log := logger.Get(ctx)

	accountKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return errors.WithStack(err)
	}

	client := &goacme.Client{
		Key: accountKey,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: config.Directory.Insecure,
				},
			},
		},
		DirectoryURL: config.Directory.DirectoryURL,
	}

	account, err := client.Register(ctx, &goacme.Account{
		Contact: []string{"mailto:wojtek@exw.co"},
	}, goacme.AcceptTOS)
	if err != nil {
		return errors.WithStack(err)
	}

	fmt.Println(account)

	waitChClosed := make(chan time.Time)
	close(waitChClosed)
	var waitCh <-chan time.Time = waitChClosed

	for {
		select {
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		case <-waitCh:
			waitCh = time.After(time.Minute)
		}

		err := parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
			startCh := make(chan struct{}, len(config.DNSACME))
			reqCh := make(chan *wire.MsgRequest, len(config.DNSACME))
			ackCh := make(chan struct{}, len(config.DNSACME))

			for _, dnsACMEAddr := range config.DNSACME {
				spawn("dnsacme", parallel.Continue, func(ctx context.Context) error {
					err := resonance.RunClient[wire.Marshaller](ctx, dnsACMEAddr, dnsacme.WireConfig,
						func(ctx context.Context, recvCh <-chan any, c *resonance.Connection[wire.Marshaller]) error {
							startCh <- struct{}{}
							var req *wire.MsgRequest

							select {
							case <-ctx.Done():
								return errors.WithStack(ctx.Err())
							case req = <-reqCh:
							}

							c.Send(req)

							msg, ok := <-recvCh
							if !ok {
								return errors.WithStack(ctx.Err())
							}
							if _, ok := msg.(*wire.MsgAck); !ok {
								return errors.New("unexpected response")
							}

							ackCh <- struct{}{}

							<-ctx.Done()
							return errors.WithStack(ctx.Err())
						},
					)
					if err != nil && !errors.Is(err, context.Canceled) {
						logger.Get(ctx).Error("DNS ACME connection failed", zap.Error(err))
					}
					return nil
				})
			}

			spawn("order", parallel.Exit, func(ctx context.Context) error {
				waitStartCh := time.After(dnsACMEConnectionTimeout)
				var started int
			startLoop:
				for range config.DNSACME {
					select {
					case <-ctx.Done():
						return errors.WithStack(ctx.Err())
					case <-waitStartCh:
						if started > 0 {
							break startLoop
						}
						return errors.New("timeout waiting on connection to dns acme")
					case <-startCh:
						if started == 0 {
							waitStartCh = time.After(dnsACMEOnboardTimeout)
						}
						started++
					}
				}

				acmeOrder, err := client.AuthorizeOrder(ctx, goacme.DomainIDs(config.Domains...))
				if err != nil {
					return errors.WithStack(err)
				}

				o := order{
					OrderURI:   acmeOrder.URI,
					Challenges: make([]challenge, 0, len(acmeOrder.AuthzURLs)),
				}
				for _, authzURL := range acmeOrder.AuthzURLs {
					authZ, err := client.GetAuthorization(ctx, authzURL)
					if err != nil {
						return errors.WithStack(err)
					}
					if authZ.Identifier.Type != "dns" {
						continue
					}

					for _, acmeChallenge := range authZ.Challenges {
						if acmeChallenge.Type != "dns-01" {
							continue
						}

						o.Challenges = append(o.Challenges, challenge{
							Domain:    authZ.Identifier.Value,
							AuthZURI:  authZ.URI,
							Challenge: acmeChallenge,
						})
					}
				}

				req := &wire.MsgRequest{
					Provider:   config.Directory.Provider,
					AccountURI: account.URI,
					Challenges: make([]wire.Challenge, 0, len(o.Challenges)),
				}
				for _, ch := range o.Challenges {
					auth, err := client.DNS01ChallengeRecord(ch.Challenge.Token)
					if err != nil {
						return errors.WithStack(err)
					}

					req.Challenges = append(req.Challenges, wire.Challenge{
						Domain: ch.Domain,
						Value:  auth,
					})
				}

				for range config.DNSACME {
					reqCh <- req
				}

				waitACKCh := time.After(dnsACMEACKTimeout)
				var acked bool
			ackLoop:
				for range started {
					select {
					case <-ctx.Done():
						return errors.WithStack(ctx.Err())
					case <-waitACKCh:
						if acked {
							break ackLoop
						}
						return errors.New("timeout waiting on ack from to dns acme")
					case <-ackCh:
						acked = true
					}
				}

				for _, ch := range o.Challenges {
					if _, err := client.Accept(ctx, ch.Challenge); err != nil {
						return errors.WithStack(err)
					}

					if _, err := client.WaitAuthorization(ctx, ch.AuthZURI); err != nil {
						return errors.WithStack(err)
					}
				}

				order, err := client.WaitOrder(ctx, o.OrderURI)
				if err != nil {
					return errors.WithStack(err)
				}

				certKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
				if err != nil {
					return errors.WithStack(err)
				}

				certReq := &x509.CertificateRequest{
					Subject:  pkix.Name{CommonName: config.Domains[0]},
					DNSNames: config.Domains,
				}
				csr, err := x509.CreateCertificateRequest(rand.Reader, certReq, certKey)
				if err != nil {
					return errors.WithStack(err)
				}

				chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
				if err != nil {
					return errors.WithStack(err)
				}

				fmt.Println(chain)

				return nil
			})

			return nil
		})

		switch {
		case err == nil:
		case errors.Is(err, ctx.Err()):
			return err
		case errors.Is(err, context.Canceled):
		default:
			log.Error("Certificate issuance failed", zap.Error(err))
		}
	}
}
