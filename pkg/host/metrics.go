package host

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func newMetrics() (*metrics, prometheus.Gatherer) {
	r := prometheus.NewRegistry()
	return &metrics{
		registry: r,
		startTime: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "start_time",
			Help: "Time when box has been started",
		}),
	}, r
}

type metrics struct {
	registry  *prometheus.Registry
	startTime prometheus.Gauge
}

// BoxStarted reports the start time of the box.
func (m *metrics) BoxStarted() {
	m.startTime.Set(float64(time.Now().UnixNano()) / 1_000_000.0)
}
