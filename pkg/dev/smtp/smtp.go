package smtp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/emersion/go-message"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/outofforest/logger"
	"github.com/outofforest/parallel"
)

func runSMTP(ctx context.Context, db *db) (retErr error) {
	s := smtp.NewServer(newSMTPBackend(logger.Get(ctx), db))
	s.Addr = fmt.Sprintf("0.0.0.0:%d", SMTPPort)
	s.WriteTimeout = time.Second
	s.ReadTimeout = time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 1
	s.AllowInsecureAuth = true

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("watchdog", parallel.Fail, func(ctx context.Context) error {
			<-ctx.Done()
			if err := s.Shutdown(ctx); err != nil && !errors.Is(err, smtp.ErrServerClosed) {
				return errors.WithStack(err)
			}
			return nil
		})
		spawn("server", parallel.Fail, func(ctx context.Context) error {
			return errors.WithStack(s.ListenAndServe())
		})
		return nil
	})
}

type smtpBackend struct {
	db  *db
	log *zap.Logger
}

func newSMTPBackend(log *zap.Logger, db *db) *smtpBackend {
	return &smtpBackend{
		db:  db,
		log: log,
	}
}

// NewSession is called after client greeting (EHLO, HELO).
func (b *smtpBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &smtpSession{
		db:  b.db,
		log: b.log,
	}, nil
}

type smtpSession struct {
	db       *db
	log      *zap.Logger
	from, to string
}

// AuthMechanisms returns a slice of available auth mechanisms; only PLAIN is
// supported in this example.
func (s *smtpSession) AuthMechanisms() []string {
	return nil
}

// Auth is the handler for supported authenticators.
func (s *smtpSession) Auth(mech string) (sasl.Server, error) {
	return nil, nil //nolint:nilnil
}

func (s *smtpSession) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *smtpSession) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.to = to
	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	mRaw, err := io.ReadAll(r)
	if err != nil {
		return errors.WithStack(err)
	}

	m, err := message.Read(bytes.NewReader(mRaw))
	if err != nil {
		return errors.WithStack(err)
	}

	s.log.Info("Email received",
		zap.String("subject", m.Header.Get("Subject")),
		zap.String("sender", s.from),
		zap.String("recipient", s.to),
	)

	return errors.WithStack(s.db.User(s.to, true).Inbox().CreateMessage(nil, time.Now(), bytes.NewReader(mRaw)))
}

func (s *smtpSession) Reset() {}

func (s *smtpSession) Logout() error {
	return nil
}
