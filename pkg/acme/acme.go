package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
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

	accountFile = "account"
	certFile    = "cert"
)

// Service returns new acme client service.
func Service(storeDir string, dirConfig DirectoryConfig, configurators ...Configurator) host.Configurator {
	return cloudless.Join(
		cloudless.Service("acme", parallel.Fail, func(ctx context.Context) error {
			config := Config{
				AccountFile: filepath.Join(storeDir, accountFile),
				CertFile:    filepath.Join(storeDir, certFile),
				Directory:   dirConfig,
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

	client := &goacme.Client{
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: config.Directory.Insecure,
				},
			},
		},
		DirectoryURL: config.Directory.DirectoryURL,
	}

	keyID, key, err := readAccount(config.AccountFile)
	if err != nil {
		var err error
		if key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader); err != nil {
			return errors.WithStack(err)
		}

		client.Key = key

		keyID, err = registerAccount(ctx, client, "wojtek@exw.co")
		if err != nil {
			return errors.WithStack(err)
		}

		if err := storeAccount(config.AccountFile, keyID, key); err != nil {
			return errors.WithStack(err)
		}
	}
	client.Key = key
	client.KID = keyID

	renewTimerFunc := renewTimerFactory(config.Directory.RenewDuration)

	expirationTime, _ := readCertificateExpirationTime(config.CertFile)
	waitCh := renewTimerFunc(expirationTime)

	log.Info("Certificate expiration time", zap.Time("expirationTime", expirationTime))

	for {
		select {
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		case <-waitCh:
		}

		log.Info("Issuing certificate")

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

			spawn("order", parallel.Exit, func(ctx context.Context) (retErr error) {
				log := logger.Get(ctx)

				defer func() {
					if retErr != nil {
						waitCh = time.After(rerunInterval)
					}
				}()

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

				switch acmeOrder.Status {
				case goacme.StatusPending:
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
							if acmeChallenge.Status != "pending" || acmeChallenge.Type != "dns-01" {
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
						AccountURI: string(client.KID),
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

					acmeOrder, err = client.WaitOrder(ctx, o.OrderURI)
					if err != nil {
						return errors.WithStack(err)
					}
				case goacme.StatusReady:
				default:
					return errors.Errorf("invalid order status %q", acmeOrder.Status)
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

				chain, _, err := client.CreateOrderCert(ctx, acmeOrder.FinalizeURL, csr, true)
				if err != nil {
					return errors.WithStack(err)
				}

				if len(chain) == 0 {
					return errors.New("empty certificate chain")
				}

				expirationTime, err := certificateExpirationTime(chain[0])
				if err != nil {
					return errors.WithStack(err)
				}

				if err := storeCertificate(config.CertFile, chain); err != nil {
					return errors.WithStack(err)
				}

				waitCh = renewTimerFunc(expirationTime)

				log.Info("Certificate issued", zap.Time("expirationTime", expirationTime))

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

type account struct {
	KeyID goacme.KeyID
	Key   []byte
}

func readAccount(accountFile string) (goacme.KeyID, *ecdsa.PrivateKey, error) {
	accountF, err := os.Open(accountFile)
	if err != nil {
		return "", nil, errors.WithStack(err)
	}
	defer accountF.Close()

	var acc account
	if err := json.NewDecoder(accountF).Decode(&acc); err != nil {
		return "", nil, errors.WithStack(err)
	}

	if acc.KeyID == "" {
		return "", nil, errors.New("empty KeyID")
	}

	key, err := x509.ParseECPrivateKey(acc.Key)
	if err != nil {
		return "", nil, errors.WithStack(err)
	}

	return acc.KeyID, key, nil
}

func storeAccount(accountFile string, keyID goacme.KeyID, key *ecdsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(accountFile), 0o700); err != nil {
		return errors.WithStack(err)
	}

	accountF, err := os.OpenFile(accountFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer accountF.Close()

	keyRaw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return errors.WithStack(err)
	}

	return errors.WithStack(json.NewEncoder(accountF).Encode(account{
		KeyID: keyID,
		Key:   keyRaw,
	}))
}

func registerAccount(ctx context.Context, client *goacme.Client, email string) (goacme.KeyID, error) {
	_, err := client.Register(ctx, &goacme.Account{
		Contact: []string{"mailto:" + email},
	}, goacme.AcceptTOS)
	if err != nil {
		return "", errors.WithStack(err)
	}

	return client.KID, nil
}

func readCertificateExpirationTime(certFile string) (time.Time, error) {
	cert, err := os.ReadFile(certFile)
	if err != nil {
		return time.Time{}, errors.WithStack(err)
	}

	block, _ := pem.Decode(cert)
	if block == nil {
		return time.Time{}, errors.New("file does not contain certificate")
	}

	return certificateExpirationTime(block.Bytes)
}

func storeCertificate(certFile string, chain [][]byte) error {
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return errors.WithStack(err)
	}

	certF, err := os.OpenFile(certFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer certF.Close()

	for _, cert := range chain {
		if err := pem.Encode(certF, &pem.Block{Type: "CERTIFICATE", Bytes: cert}); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func certificateExpirationTime(cert []byte) (time.Time, error) {
	c, err := x509.ParseCertificate(cert)
	if err != nil {
		return time.Time{}, errors.WithStack(err)
	}

	if time.Until(c.NotAfter) < 0 {
		return time.Time{}, errors.New("certificate expired")
	}

	return c.NotAfter, nil
}

func renewTimerFactory(renewBefore time.Duration) func(time.Time) <-chan time.Time {
	return func(expirationTime time.Time) <-chan time.Time {
		renewDuration := time.Until(expirationTime)
		if renewDuration <= renewBefore {
			ch := make(chan time.Time)
			close(ch)
			return ch
		}

		return time.After(renewDuration - renewBefore)
	}
}
