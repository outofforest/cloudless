package eye

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

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/parallel"
)

const collectInterval = time.Second

type measure struct {
	total uint64
	idle  uint64
}

// CollectService returns new service collecting system metrics.
func CollectService() host.Configurator {
	metrics, gatherer := newMetrics()

	return cloudless.Join(
		cloudless.Metrics(gatherer),
		cloudless.Service("eye", parallel.Fail, func(ctx context.Context) error {
			timer := time.NewTicker(collectInterval)
			defer timer.Stop()

			var previous, current []measure

			statF, err := os.Open("/proc/stat")
			if err != nil {
				return errors.WithStack(err)
			}
			defer statF.Close()

			reader := bufio.NewReader(statF)

			for {
				select {
				case <-ctx.Done():
					return errors.WithStack(ctx.Err())
				case <-timer.C:
				}

				previous, current = current, previous[:0]

				if _, err := statF.Seek(0, io.SeekStart); err != nil {
					return errors.WithStack(err)
				}
				reader.Reset(statF)

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
						metrics.CPUUtilizationAll(util)
						continue
					}
					if util > worst {
						worst = util
					}
				}

				metrics.CPUUtilizationWorst(worst)
			}
		}),
	)
}

func newMetrics() (*metrics, prometheus.Gatherer) {
	r := prometheus.NewRegistry()
	return &metrics{
		registry: r,
		cpuUtilizationAll: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Subsystem: "eye",
			Name:      "cpu_utilization_all",
			Help:      "Utilization of all CPUs",
		}),
		cpuUtilizationWorst: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Subsystem: "eye",
			Name:      "cpu_utilization_worst",
			Help:      "Utilization of the worst CPUs",
		}),
	}, r
}

type metrics struct {
	registry            *prometheus.Registry
	cpuUtilizationAll   prometheus.Gauge
	cpuUtilizationWorst prometheus.Gauge
}

func (m *metrics) CPUUtilizationAll(v float64) {
	m.cpuUtilizationAll.Set(v)
}

func (m *metrics) CPUUtilizationWorst(v float64) {
	m.cpuUtilizationWorst.Set(v)
}
