package mounts

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sys/unix"

	"github.com/outofforest/parallel"
)

const (
	namespace = "eye"
	subsystem = "mounts"
)

var ignore = map[string]struct{}{
	"/dev":  {},
	"/proc": {},
	"/sys":  {},
}

// New returns mounts collector.
func New(collectInterval time.Duration) func() (string, prometheus.Gatherer, parallel.Task) {
	return func() (string, prometheus.Gatherer, parallel.Task) {
		m, gatherer := newMetrics()

		return "mounts", gatherer, func(ctx context.Context) error {
			timer := time.NewTicker(collectInterval)
			defer timer.Stop()

			f, err := os.Open("/proc/mounts")
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			reader := bufio.NewReader(f)

			for {
				select {
				case <-ctx.Done():
					return errors.WithStack(ctx.Err())
				case <-timer.C:
				}

				if _, err := f.Seek(0, io.SeekStart); err != nil {
					return errors.WithStack(err)
				}
				reader.Reset(f)

			loop:
				for {
					line, err := reader.ReadString('\n')
					switch {
					case errors.Is(err, io.EOF):
						break loop
					case err != nil:
						return errors.WithStack(err)
					}

					props := strings.SplitN(line, " ", 3)
					if len(props) < 2 {
						// last empty line
						break
					}
					mountPoint := props[1]
					if _, exists := ignore[mountPoint]; exists {
						continue
					}

					var stats unix.Statfs_t
					if err := unix.Statfs(mountPoint, &stats); err != nil {
						return errors.WithStack(err)
					}

					if stats.Blocks == 0 {
						continue
					}

					m.Utilization(mountPoint, float64(stats.Blocks-stats.Bfree)/float64(stats.Blocks))
				}
			}
		}
	}
}

const labelMountpoint = "mountpoint"

func newMetrics() (*metrics, prometheus.Gatherer) {
	r := prometheus.NewRegistry()
	return &metrics{
		registry: r,
		utilization: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "utilization",
			Help:      "Utilization of mountpoint",
		}, []string{labelMountpoint}),
	}, r
}

type metrics struct {
	registry    *prometheus.Registry
	utilization *prometheus.GaugeVec
}

func (m *metrics) Utilization(mountpoint string, v float64) {
	m.utilization.WithLabelValues(mountpoint).Set(v)
}
