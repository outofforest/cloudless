package mailer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
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
	"github.com/outofforest/wave"
)

// Service returns new acme client service.
func Service(appName, email, hostname string, configurators ...Configurator) host.Configurator {
	return cloudless.Service("mailer", parallel.Fail, func(ctx context.Context) error {
		log := logger.Get(ctx)

		var config Config

		for _, configurator := range configurators {
			configurator(&config)
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

		var timeBytes [8]byte
		binary.BigEndian.PutUint64(timeBytes[:], uint64(time.Now().Unix()))
		provider := appName + "-" + hex.EncodeToString(timeBytes[:])

		parts := strings.SplitN(email, "@", 2)
		domain := parts[1]

		log.Info("Starting mailer", zap.String("dkimDomain", dnsdkim.Domain(provider, domain)))

		return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
			waveClient, _, err := wave.NewClient(wave.ClientConfig{
				Servers:        config.WaveServers,
				MaxMessageSize: 1024,
			})
			if err != nil {
				return err
			}

			resolver := net.DefaultResolver
			if len(config.DNSServers) > 0 {
				dialer := &net.Dialer{}
				resolver = &net.Resolver{
					PreferGo: false,
					Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
						conn, err := dialer.DialContext(ctx, network, config.DNSServers[0])
						return conn, errors.WithStack(err)
					},
				}
			}

			spawn("wave", parallel.Fail, waveClient.Run)
			spawn("dkim", parallel.Fail, func(ctx context.Context) error {
				m := wire.NewMarshaller()
				for {
					if err := waveClient.Send(&wire.MsgRequest{
						Provider:  provider,
						PublicKey: pubKey,
					}, m); err != nil {
						return err
					}

					select {
					case <-ctx.Done():
						return errors.WithStack(ctx.Err())
					case <-time.After(dnsdkim.RefreshInterval):
					}
				}
			})
			spawn("mailer", parallel.Fail, func(ctx context.Context) error {
				log := logger.Get(ctx)

				for {
					select {
					case <-ctx.Done():
						return errors.WithStack(ctx.Err())
					case <-time.After(10 * time.Second):
					}

					log.Info("Sending mailer.")

					mxs, err := resolver.LookupMX(ctx, domain)
					if err != nil {
						log.Error("Resolving MX failed.", zap.Error(err))
						continue
					}

					// Create the mailer message
					msg := mail.NewMsg(mail.WithNoDefaultUserAgent())
					msg.SetMessageIDWithValue(uuid.New().String() + "@" + domain)

					if err := msg.FromFormat("Dev Sender", email); err != nil {
						log.Error("Mail error", zap.Error(err))
						continue
					}
					if err := msg.To(email); err != nil {
						log.Error("Mail error", zap.Error(err))
						continue
					}
					msg.Subject("This is test message")
					msg.SetBodyString(mail.TypeTextPlain, "Test message")

					// Add DKIM signing middleware to the mailer
					dkimConfig, err := dkim.NewConfig(domain, provider)
					if err != nil {
						log.Error("Mail error", zap.Error(err))
						continue
					}

					middleware, err := dkim.NewFromRSAKey(privKeyPEM, dkimConfig)
					if err != nil {
						log.Error("Mail error", zap.Error(err))
						continue
					}

					// Apply the DKIM middleware to sign the mailer
					msg = middleware.Handle(msg)

					client, err := mail.NewClient(mxs[0].Host, mail.WithPort(25), mail.WithTLSPolicy(mail.TLSOpportunistic),
						mail.WithHELO(hostname), mail.WithDialContextFunc(dialFunc(resolver)), mail.WithoutNoop())
					if err != nil {
						log.Error("Mail error", zap.Error(err))
						continue
					}

					if err := client.DialAndSendWithContext(ctx, msg); err != nil {
						log.Error("Mail error", zap.Error(err))
						continue
					}

					log.Info("Email sent.")
				}
			})

			return nil
		})
	})
}

func dialFunc(resolver *net.Resolver) mail.DialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		parts := strings.SplitN(address, ".:", 2)
		if len(parts) != 2 {
			return nil, errors.WithStack(fmt.Errorf("invalid address: %s", address))
		}

		address = parts[0]
		port := parts[1]

		ips, err := resolver.LookupIP(ctx, "ip", address)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		if len(ips) == 0 {
			return nil, errors.WithStack(fmt.Errorf("no IP addresses found for %s", address))
		}

		d := &net.Dialer{}
		conn, err := d.DialContext(ctx, network, ips[0].String()+":"+port)
		return conn, errors.WithStack(err)
	}
}
