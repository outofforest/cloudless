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
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
	"github.com/outofforest/resonance"
)

// Service returns new acme client service.
func Service(dnsACMEAddr string, domains ...string) host.Configurator {
	return cloudless.Join(
		cloudless.Service("acme", parallel.Fail, func(ctx context.Context) error {
			if len(domains) == 0 {
				return errors.New("no domains defined")
			}

			for {
				if err := run(ctx, dnsACMEAddr, domains); err != nil {
					if errors.Is(err, ctx.Err()) {
						return err
					}
					logger.Get(ctx).Error("ACME failed", zap.Error(err))
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

func run(ctx context.Context, dnsACMEAddr string, domains []string) error {
	accountKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return errors.WithStack(err)
	}

	client := &goacme.Client{
		Key: accountKey,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
		DirectoryURL: fmt.Sprintf("https://10.0.2.5:%d/dir", pebble.Port),
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

		if err := resonance.RunClient[wire.Marshaller](ctx, dnsACMEAddr, dnsacme.WireConfig,
			func(ctx context.Context, recvCh <-chan any, c *resonance.Connection[wire.Marshaller]) error {
				acmeOrder, err := client.AuthorizeOrder(ctx, goacme.DomainIDs(domains...))
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

				c.Send(req)

				msg, ok := <-recvCh
				if !ok {
					return nil
				}
				if _, ok := msg.(*wire.MsgAck); !ok {
					return errors.New("unexpected response")
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
					Subject:  pkix.Name{CommonName: domains[0]},
					DNSNames: domains,
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
			},
		); err != nil {
			return err
		}
	}
}
