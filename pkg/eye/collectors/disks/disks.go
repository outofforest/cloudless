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

	"github.com/outofforest/cloudless/pkg/eye/collectors"
	"github.com/outofforest/cloudless/pkg/eye/metrics"
	"github.com/outofforest/parallel"
)

const (
	namespace = "eye"
	subsystem = "disks"
	labelDev  = "dev"

	indexDevName      = 2
	indexIOInProgress = 11
)

var indexes = map[int]struct{}{
	indexDevName:      {},
	indexIOInProgress: {},
}

// New returns network collector.
func New(collectInterval time.Duration) collectors.CollectorFunc {
	return func() (string, *metrics.Set, parallel.Task) {
		set := metrics.NewSet()

		return "disks", set, func(ctx context.Context) error {
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

							// Number of I/O operations in progress.
							set.GetOrCreateGauge(metrics.N(namespace, subsystem, "in_progress"),
								metrics.L(labelDev, devName)).Set(float64(value))
						}
					}
				}
			}
		}
	}
}
