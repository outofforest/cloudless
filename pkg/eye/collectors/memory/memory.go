package memory

import (
	"bufio"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/outofforest/parallel"
)

const (
	namespace = "eye"
	subsystem = "memory"

	fieldMemTotal       = "MemTotal"
	fieldMemAvailable   = "MemAvailable"
	fieldHugePagesTotal = "HugePages_Total"
	fieldHugePageSize   = "Hugepagesize"
)

var fields = map[string]struct{}{
	fieldMemTotal:       {},
	fieldMemAvailable:   {},
	fieldHugePagesTotal: {},
	fieldHugePageSize:   {},
}

// New returns memory collector.
func New(collectInterval time.Duration) func() (string, prometheus.Gatherer, parallel.Task) {
	return func() (string, prometheus.Gatherer, parallel.Task) {
		m, gatherer := newMetrics()

		return "memory", gatherer, func(ctx context.Context) error {
			timer := time.NewTicker(collectInterval)
			defer timer.Stop()

			f, err := os.Open("/proc/meminfo")
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

				var collectedValues int

				var memTotal, memAvailable, hugePagesTotal, hugePageSize uint64
			loop:
				for {
					line, err := reader.ReadString('\n')
					switch {
					case errors.Is(err, io.EOF):
						break loop
					case err != nil:
						return errors.WithStack(err)
					}

					pos := strings.IndexByte(line, ':')
					if pos < 0 {
						continue
					}

					name := line[:pos]
					if _, exists := fields[name]; !exists {
						continue
					}

					value, err := strconv.ParseUint(strings.TrimSpace(strings.TrimSuffix(line[pos+1:], "kB\n")),
						10, 64)
					if err != nil {
						return errors.WithStack(err)
					}

					collectedValues++
					if collectedValues == len(fields) {
						break
					}

					switch name {
					case fieldMemTotal:
						memTotal = value
					case fieldMemAvailable:
						memAvailable = value
					case fieldHugePagesTotal:
						hugePagesTotal = value
					case fieldHugePageSize:
						hugePageSize = value
					}
				}

				if collectedValues != len(fields) {
					return errors.New("some values are missing")
				}
				if memTotal == 0 {
					return errors.New("memTotal is zero")
				}

				memHugePages := hugePageSize * hugePagesTotal
				if memHugePages > memTotal {
					return errors.New("memHugePages is greater than memTotal")
				}

				memTotal -= memHugePages
				if memAvailable > memTotal {
					return errors.New("memAvailable is greater than memTotal")
				}

				m.Utilization(float64(memTotal-memAvailable) / float64(memTotal))
			}
		}
	}
}

func newMetrics() (*metrics, prometheus.Gatherer) {
	r := prometheus.NewRegistry()
	return &metrics{
		registry: r,
		utilization: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "utilization",
			Help:      "Utilization of memory",
		}),
	}, r
}

type metrics struct {
	registry    *prometheus.Registry
	utilization prometheus.Gauge
}

func (m *metrics) Utilization(v float64) {
	m.utilization.Set(v)
}
