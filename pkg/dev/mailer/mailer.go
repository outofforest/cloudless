package mailer

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/wneessen/go-mail"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless"
	dnsdkim "github.com/outofforest/cloudless/pkg/dns/dkim"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/mailing"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
	"github.com/outofforest/wave"
)

// Service returns new acme client service.
func Service(appName, email string, config mailing.Config) host.Configurator {
	return cloudless.Service("mailer", func(ctx context.Context) error {
		log := logger.Get(ctx)

		dkimConfig := dnsdkim.NewConfig(appName)

		log.Info("Starting mailer.")

		return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
			waveClient, _, err := wave.NewClient(wave.ClientConfig{
				CA:             config.Wave.CA,
				Servers:        config.Wave.Servers,
				MaxMessageSize: config.Wave.MaxMessageSize,
			})
			if err != nil {
				return err
			}

			spawn("wave", parallel.Fail, waveClient.Run)
			spawn("dkim", parallel.Fail, func(ctx context.Context) error {
				return dnsdkim.RunClient(ctx, waveClient, dkimConfig)
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

					// Create the mailer message
					msg := mailing.NewMessage()

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

					if err := mailing.SendMessage(ctx, config, dkimConfig, msg); err != nil {
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
