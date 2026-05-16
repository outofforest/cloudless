package ntp

import (
	"context"
	"math/rand"
	"syscall"
	"time"

	"github.com/beevik/ntp"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/logger"
)

var servers = []string{
	"0.fedora.pool.ntp.org",
	"1.fedora.pool.ntp.org",
	"2.fedora.pool.ntp.org",
	"3.fedora.pool.ntp.org",
	"0.pl.pool.ntp.org",
	"1.pl.pool.ntp.org",
	"2.pl.pool.ntp.org",
	"3.pl.pool.ntp.org",
	"0.europe.pool.ntp.org",
	"1.europe.pool.ntp.org",
	"2.europe.pool.ntp.org",
	"3.europe.pool.ntp.org",
	"0.de.pool.ntp.org",
	"1.de.pool.ntp.org",
	"2.de.pool.ntp.org",
	"3.de.pool.ntp.org",
}

// Service creates new NTP service.
func Service() host.Configurator {
	return cloudless.Service("ntp", func(ctx context.Context) error {
		log := logger.Get(ctx)
		rnd := rand.New(rand.NewSource(time.Now().Unix()))
		for {
			server := servers[rnd.Intn(len(servers))]
			resp, err := ntp.Query(server)
			if err != nil {
				select {
				case <-ctx.Done():
					return errors.WithStack(ctx.Err())
				case <-time.After(10 * time.Second):
					log.Error("Getting time from NTP server failed.", zap.String("server", server),
						zap.Error(err))
					continue
				}
			}

			if err := syscall.Settimeofday(&syscall.Timeval{
				Sec: time.Now().Add(resp.ClockOffset).Unix(),
			}); err != nil {
				return errors.WithStack(err)
			}

			select {
			case <-ctx.Done():
				return errors.WithStack(ctx.Err())
			case <-time.After(time.Hour):
			}
		}
	})
}
