package smtp

import (
	"context"
	"fmt"

	"github.com/phires/go-guerrilla"
	"github.com/phires/go-guerrilla/backends"
	"github.com/phires/go-guerrilla/mail"
	"github.com/phires/go-guerrilla/response"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

const (
	// Port is the port SMTP service listnes on.
	Port = 25

	hostname = "testing.local"
)

// Service returns DNS service.
func Service(configurators ...Configurator) host.Configurator {
	config := Config{
		Port: Port,
	}
	for _, configurator := range configurators {
		configurator(&config)
	}

	return cloudless.Service("smtp", parallel.Fail, func(ctx context.Context) error {
		return run(ctx, config)
	})
}

func run(ctx context.Context, config Config) (retErr error) {
	smtpCfg := &guerrilla.AppConfig{
		AllowedHosts: config.AllowedHosts,
		LogFile:      "off",
		Servers: []guerrilla.ServerConfig{
			{
				IsEnabled:       true,
				ListenInterface: fmt.Sprintf("0.0.0.0:%d", Port),
				Hostname:        hostname,
				LogFile:         "off",
			},
		},
	}

	d := guerrilla.Daemon{Config: smtpCfg}
	d.Backend = &backend{
		log: logger.Get(ctx),
	}
	if err := d.Start(); err != nil {
		return err
	}

	<-ctx.Done()
	d.Shutdown()
	return errors.WithStack(ctx.Err())
}

type backend struct {
	log *zap.Logger
}

// Process processes then saves the mail envelope.
func (b *backend) Process(e *mail.Envelope) backends.Result {
	if err := e.ParseHeaders(); err != nil {
		return backends.NewResult(response.Canned.SuccessMessageQueued, response.SP, "12345")
	}

	b.log.Info("Email received", zap.String("subject", e.Subject),
		zap.String("sender", e.MailFrom.String()))
	return backends.NewResult(response.Canned.SuccessMessageQueued, response.SP, "12345")
}

// ValidateRcpt validates the last recipient that was pushed to the mail envelope.
func (b *backend) ValidateRcpt(e *mail.Envelope) backends.RcptError {
	return nil
}

// Initialize initializes the backend, eg. creates folders, sets-up database connections.
func (b *backend) Initialize(backends.BackendConfig) error {
	return nil
}

// Reinitialize initializes the backend after it was Shutdown().
func (b *backend) Reinitialize() error {
	return nil
}

// Shutdown frees / closes anything created during initializations.
func (b *backend) Shutdown() error {
	return nil
}

// Start Starts a backend that has been initialized.
func (b *backend) Start() error {
	return nil
}
