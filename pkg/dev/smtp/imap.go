package smtp

import (
	"context"
	"fmt"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/server"
	"github.com/pkg/errors"

	"github.com/outofforest/parallel"
)

func runIMAP(ctx context.Context, db *db) (retErr error) {
	s := server.New(newIMAPBackend(db))
	s.Addr = fmt.Sprintf("0.0.0.0:%d", IMAPPort)
	s.AllowInsecureAuth = true

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("watchdog", parallel.Fail, func(ctx context.Context) error {
			<-ctx.Done()
			return errors.WithStack(s.Close())
		})
		spawn("server", parallel.Fail, func(ctx context.Context) error {
			return errors.WithStack(s.ListenAndServe())
		})
		return nil
	})
}

type imapBackend struct {
	db *db
}

func newIMAPBackend(db *db) *imapBackend {
	return &imapBackend{
		db: db,
	}
}

func (b *imapBackend) Login(_ *imap.ConnInfo, username, password string) (backend.User, error) {
	return b.db.User(username, false), nil
}
