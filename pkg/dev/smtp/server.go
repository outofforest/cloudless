package smtp

import (
	"context"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/parallel"
)

const (
	// SMTPPort is the port SMTP service listens on.
	SMTPPort = 25

	// IMAPPort is the port IMAP service listens on.
	IMAPPort = 143
)

// Service returns DNS service.
func Service() host.Configurator {
	db := newDB()

	return cloudless.Join(
		cloudless.Service("smtp", parallel.Fail, func(ctx context.Context) error {
			return runSMTP(ctx, db)
		}),
		cloudless.Service("imap", parallel.Fail, func(ctx context.Context) error {
			return runIMAP(ctx, db)
		}),
	)
}
