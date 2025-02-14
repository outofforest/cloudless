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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/outofforest/parallel"
)

const (
	namespace = "eye"
	subsystem = "network"

	indexReceivedBytes    = 0
	indexTransmittedBytes = 8
)

var indexes = map[int]struct{}{
	indexReceivedBytes:    {},
	indexTransmittedBytes: {},
}

// New returns network collector.
func New(collectInterval time.Duration) func() (string, prometheus.Gatherer, parallel.Task) {
	return func() (string, prometheus.Gatherer, parallel.Task) {
		m, gatherer := newMetrics()

		return "network", gatherer, func(ctx context.Context) error {
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
							m.ReceivedBytes(iface, value)
						case indexTransmittedBytes:
							m.TransmittedBytes(iface, value)
						}
					}
				}
			}
		}
	}
}

const labelIface = "iface"

func newMetrics() (*metrics, prometheus.Gatherer) {
	r := prometheus.NewRegistry()
	return &metrics{
		registry: r,
		receivedBytes: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "received_bytes_total",
			Help:      "Number of bytes received",
		}, []string{labelIface}),
		transmittedBytes: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "transmitted_bytes_total",
			Help:      "Number of bytes transmitted",
		}, []string{labelIface}),
	}, r
}

type metrics struct {
	registry         *prometheus.Registry
	receivedBytes    *prometheus.GaugeVec
	transmittedBytes *prometheus.GaugeVec
}

func (m *metrics) ReceivedBytes(iface string, bytes uint64) {
	m.receivedBytes.WithLabelValues(iface).Set(float64(bytes))
}

func (m *metrics) TransmittedBytes(iface string, bytes uint64) {
	m.transmittedBytes.WithLabelValues(iface).Set(float64(bytes))
}
