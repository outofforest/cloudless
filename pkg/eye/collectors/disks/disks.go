package disks

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
	subsystem = "disks"

	indexDevName      = 2
	indexIOInProgress = 11
)

var indexes = map[int]struct{}{
	indexDevName:      {},
	indexIOInProgress: {},
}

// New returns network collector.
func New(collectInterval time.Duration) func() (string, prometheus.Gatherer, parallel.Task) {
	return func() (string, prometheus.Gatherer, parallel.Task) {
		m, gatherer := newMetrics()

		return "disks", gatherer, func(ctx context.Context) error {
			timer := time.NewTicker(collectInterval)
			defer timer.Stop()

			f, err := os.Open("/proc/diskstats")
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

					var devName string
					for i := 0; i <= indexIOInProgress; i++ {
						line = strings.TrimSpace(line)
						pos := strings.IndexByte(line, ' ')
						if pos < 0 {
							return errors.New("unexpected end of line")
						}
						valueStr := line[:pos]
						line = line[pos+1:]

						if _, exists := indexes[i]; !exists {
							continue
						}

						switch i {
						case indexDevName:
							devName = valueStr
						case indexIOInProgress:
							value, err := strconv.ParseUint(valueStr, 10, 64)
							if err != nil {
								return errors.WithStack(err)
							}
							m.OperationsInProgress(devName, value)
						}
					}
				}
			}
		}
	}
}

const labelDev = "dev"

func newMetrics() (*metrics, prometheus.Gatherer) {
	r := prometheus.NewRegistry()
	return &metrics{
		registry: r,
		operationsInProgress: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "in_progress",
			Help:      "Number of I/O operations in progress",
		}, []string{labelDev}),
	}, r
}

type metrics struct {
	registry             *prometheus.Registry
	operationsInProgress *prometheus.GaugeVec
}

func (m *metrics) OperationsInProgress(dev string, ops uint64) {
	m.operationsInProgress.WithLabelValues(dev).Set(float64(ops))
}
