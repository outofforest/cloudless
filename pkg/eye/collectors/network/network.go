package network

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
	namespace  = "eye"
	subsystem  = "network"
	labelIface = "iface"

	indexReceivedBytes    = 0
	indexTransmittedBytes = 8
)

var indexes = map[int]struct{}{
	indexReceivedBytes:    {},
	indexTransmittedBytes: {},
}

// New returns network collector.
func New(collectInterval time.Duration) collectors.CollectorFunc {
	return func() (string, *metrics.Set, parallel.Task) {
		set := metrics.NewSet()

		return "network", set, func(ctx context.Context) error {
			timer := time.NewTicker(collectInterval)
			defer timer.Stop()

			f, err := os.Open("/proc/net/dev")
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

					pos := strings.IndexByte(line, ':')
					if pos < 0 {
						continue
					}

					iface := strings.TrimSpace(line[:pos])

					for i := 0; i <= indexTransmittedBytes; i++ {
						line = strings.TrimSpace(line[pos+1:])
						pos = strings.IndexByte(line, ' ')
						if pos < 0 {
							return errors.New("unexpected end of line")
						}

						if _, exists := indexes[i]; !exists {
							continue
						}

						value, err := strconv.ParseUint(strings.TrimSpace(line[:pos]), 10, 64)
						if err != nil {
							return errors.WithStack(err)
						}

						switch i {
						case indexReceivedBytes:
							// Number of bytes received.
							set.GetOrCreateGauge(metrics.N(namespace, subsystem, "received_bytes_total"),
								metrics.L(labelIface, iface)).Set(float64(value))
						case indexTransmittedBytes:
							// Number of bytes transmitted.
							set.GetOrCreateGauge(metrics.N(namespace, subsystem, "transmitted_bytes_total"),
								metrics.L(labelIface, iface)).Set(float64(value))
						}
					}
				}
			}
		}
	}
}
