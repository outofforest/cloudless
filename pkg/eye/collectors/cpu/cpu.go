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

	"github.com/outofforest/cloudless/pkg/eye/collectors"
	"github.com/outofforest/cloudless/pkg/eye/metrics"
	"github.com/outofforest/parallel"
)

const (
	namespace = "eye"
	subsystem = "cpu"
)

// New returns CPU collector.
func New(collectInterval time.Duration) collectors.CollectorFunc {
	return func() (string, *metrics.Set, parallel.Task) {
		set := metrics.NewSet()

		return "cpu", set, func(ctx context.Context) error {
			// Utilization of all CPUs.
			mUtilizationAll := set.NewGauge(metrics.N(namespace, subsystem, "utilization_all"))

			// Utilization of the busiest CPUs.
			mUtilizationBusiest := set.NewGauge(metrics.N(namespace, subsystem, "utilization_busiest"))

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
						mUtilizationAll.Set(util)
						continue
					}
					if util > worst {
						worst = util
					}
				}

				mUtilizationBusiest.Set(worst)
			}
		}
	}
}

type measure struct {
	total uint64
	idle  uint64
}
