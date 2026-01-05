package email

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/wneessen/go-mail"
	"github.com/wneessen/go-mail-middleware/dkim"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless"
	dnsdkim "github.com/outofforest/cloudless/pkg/dns/dkim"
	"github.com/outofforest/cloudless/pkg/dns/dkim/wire"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
	"github.com/outofforest/resonance"
)

// Service returns new acme client service.
func Service(configurators ...Configurator) host.Configurator {
	return cloudless.Join(
		cloudless.Service("acme", parallel.Fail, func(ctx context.Context) error {
			var config Config

			for _, configurator := range configurators {
				configurator(&config)
			}

			if len(config.DNSDKIM) == 0 {
				return errors.New("no dns dkim endpoints defined")
			}

			// TODO (wojciech): Change to ED25519 once smtp servers support it finally.
			privKey, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				return errors.WithStack(err)
			}

			privKeyBytes := x509.MarshalPKCS1PrivateKey(privKey)
			privKeyPEM := pem.EncodeToMemory(&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: privKeyBytes,
			})

			pubKey, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
			if err != nil {
				return errors.WithStack(err)
			}

			return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
				spawn("email", parallel.Fail, func(ctx context.Context) error {
					log := logger.Get(ctx)

					for {
						select {
						case <-ctx.Done():
							return errors.WithStack(ctx.Err())
						case <-time.After(time.Minute):
						}

						log.Info("Sending email.")

						mxs, err := net.DefaultResolver.LookupMX(ctx, "gmail.com")
						if err != nil {
							log.Error("Resolving MX failed.", zap.Error(err))
							continue
						}

						for _, mx := range mxs {
							fmt.Printf("%#v\n", mx)
						}

						// Create the email message
						email := mail.NewMsg(mail.WithNoDefaultUserAgent())
						email.SetMessageIDWithValue(uuid.New().String() + "@mail.dev.onem.network")

						if err := email.FromFormat("Wojciech Małota-Wójcik", "wojtek@dev.onem.network"); err != nil {
							log.Error("Mail error", zap.Error(err))
							continue
						}
						if err := email.To("wojciech.malota.wojcik@gmail.com"); err != nil {
							log.Error("Mail error", zap.Error(err))
							continue
						}
						email.Subject("This is test message")
						email.SetBodyString(mail.TypeTextPlain, "Test message\n\n-- \nWojciech")

						// Add DKIM signing middleware to the email
						dkimConfig, err := dkim.NewConfig("dev.onem.network", "cloudless")
						if err != nil {
							log.Error("Mail error", zap.Error(err))
							continue
						}

						middleware, err := dkim.NewFromRSAKey(privKeyPEM, dkimConfig)
						if err != nil {
							log.Error("Mail error", zap.Error(err))
							continue
						}

						// Apply the DKIM middleware to sign the email
						email = middleware.Handle(email)

						client, err := mail.NewClient(mxs[0].Host, mail.WithPort(25), mail.WithTLSPolicy(mail.TLSOpportunistic),
							mail.WithHELO("mail.dev.onem.network"))
						if err != nil {
							log.Error("Mail error", zap.Error(err))
							continue
						}

						if err := client.DialAndSendWithContext(ctx, email); err != nil {
							log.Error("Mail error", zap.Error(err))
							continue
						}

						log.Info("Email sent.")
					}
				})

				m := wire.NewMarshaller()
				for _, dnsDKIM := range config.DNSDKIM {
					spawn("dnsDKIM", parallel.Fail, func(ctx context.Context) error {
						log := logger.Get(ctx)

						for {
							err := resonance.RunClient(ctx, dnsDKIM, dnsdkim.WireConfig,
								func(ctx context.Context, c *resonance.Connection) error {
									if err := c.SendProton(&wire.MsgRequest{
										Provider:  "cloudless",
										PublicKey: pubKey,
									}, m); err != nil {
										return err
									}

									if _, err := c.ReceiveProton(m); err != nil {
										return err
									}

									<-ctx.Done()
									return errors.WithStack(ctx.Err())
								},
							)

							switch {
							case err == nil:
							case errors.Is(err, ctx.Err()):
								return errors.WithStack(ctx.Err())
							default:
								log.Error("DNS DKIM connection failed.", zap.String("server", dnsDKIM),
									zap.Error(err))
							}

							select {
							case <-ctx.Done():
								return errors.WithStack(ctx.Err())
							case <-time.After(10 * time.Second):
							}
						}
					})
				}

				return nil
			})
		}),
	)
}
