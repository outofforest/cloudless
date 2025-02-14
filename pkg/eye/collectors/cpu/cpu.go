package cpu

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
	subsystem = "cpu"
)

// New returns CPU collector.
func New(collectInterval time.Duration) func() (string, prometheus.Gatherer, parallel.Task) {
	return func() (string, prometheus.Gatherer, parallel.Task) {
		m, gatherer := newMetrics()

		return "cpu", gatherer, func(ctx context.Context) error {
			timer := time.NewTicker(collectInterval)
			defer timer.Stop()

			var previous, current []measure

			f, err := os.Open("/proc/stat")
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

				previous, current = current, previous[:0]

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
					case !strings.HasPrefix(line, "cpu"):
						break loop
					}

					var total, idle uint64
					values := strings.Split(strings.TrimSpace(line), " ")
					vi := 0
					for _, v := range values[1:] {
						if v == "" {
							continue
						}
						vi++

						n, err := strconv.ParseUint(v, 10, 64)
						if err != nil {
							return errors.WithStack(err)
						}
						total += n
						if vi == 4 {
							idle = n
						}
					}

					current = append(current, measure{
						total: total,
						idle:  idle,
					})
				}

				if len(current) == 0 || len(current) != len(previous) {
					continue
				}

				var worst float64
				for i, c := range current {
					p := previous[i]

					if c.total <= p.total || c.idle < p.idle {
						continue
					}

					total := c.total - p.total
					idle := c.idle - p.idle

					util := float64(total-idle) / float64(total)
					if i == 0 {
						m.UtilizationAll(util)
						continue
					}
					if util > worst {
						worst = util
					}
				}

				m.UtilizationBusiest(worst)
			}
		}
	}
}

type measure struct {
	total uint64
	idle  uint64
}

func newMetrics() (*metrics, prometheus.Gatherer) {
	r := prometheus.NewRegistry()
	return &metrics{
		registry: r,
		utilizationAll: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "utilization_all",
			Help:      "Utilization of all CPUs",
		}),
		utilizationBusiest: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "utilization_busiest",
			Help:      "Utilization of the busiest CPUs",
		}),
	}, r
}

type metrics struct {
	registry           *prometheus.Registry
	utilizationAll     prometheus.Gauge
	utilizationBusiest prometheus.Gauge
}

func (m *metrics) UtilizationAll(v float64) {
	m.utilizationAll.Set(v)
}

func (m *metrics) UtilizationBusiest(v float64) {
	m.utilizationBusiest.Set(v)
}
