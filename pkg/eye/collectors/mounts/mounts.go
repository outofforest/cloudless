package mounts

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/outofforest/cloudless/pkg/eye/collectors"
	"github.com/outofforest/cloudless/pkg/eye/metrics"
	"github.com/outofforest/parallel"
)

const (
	namespace       = "eye"
	subsystem       = "mounts"
	labelMountpoint = "mountpoint"
)

var ignore = map[string]struct{}{
	"/dev":  {},
	"/proc": {},
	"/sys":  {},
}

// New returns mounts collector.
func New(collectInterval time.Duration) collectors.CollectorFunc {
	return func() (string, *metrics.Set, parallel.Task) {
		set := metrics.NewSet()

		return "mounts", set, func(ctx context.Context) error {
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

					// "Utilization of mountpoint".
					set.GetOrCreateGauge(metrics.N(namespace, subsystem, "utilization"),
						metrics.L(labelMountpoint, mountPoint)).
						Set(float64(stats.Blocks-stats.Bfree) / float64(stats.Blocks))
				}
			}
		}
	}
}
