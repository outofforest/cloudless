package metrics

import (
	"io"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"
)

// Set is the wrapper around metric set, adding standard labels to all the metrics.
type Set struct {
	set    *metrics.Set
	labels []Label
}

// NewSet creates new metric set.
func NewSet() *Set {
	return &Set{
		set: metrics.NewSet(),
	}
}

// AddLabels defines labels which are added to all the metrics in the set.
func (s *Set) AddLabels(labels ...Label) {
	s.labels = append(s.labels, labels...)
}

// WritePrometheus writes all the metrics from s to w in Prometheus format.
func (s *Set) WritePrometheus(w io.Writer) {
	s.set.WritePrometheus(w)
}

// NewHistogram creates and returns new histogram in s with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned histogram is safe to use from concurrent goroutines.
func (s *Set) NewHistogram(name string, labels ...Label) *metrics.Histogram {
	return s.set.NewHistogram(buildName(name, s.labels, labels))
}

// GetOrCreateHistogram returns registered histogram in s with the given name
// or creates new histogram if s doesn't contain histogram with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned histogram is safe to use from concurrent goroutines.
//
// Performance tip: prefer NewHistogram instead of GetOrCreateHistogram.
func (s *Set) GetOrCreateHistogram(name string, labels ...Label) *metrics.Histogram {
	return s.set.GetOrCreateHistogram(buildName(name, s.labels, labels))
}

// NewPrometheusHistogram creates and returns new PrometheusHistogram in s
// with the given name and PrometheusHistogramDefaultBuckets.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned histogram is safe to use from concurrent goroutines.
func (s *Set) NewPrometheusHistogram(name string, labels ...Label) *metrics.PrometheusHistogram {
	return s.set.NewPrometheusHistogram(buildName(name, s.labels, labels))
}

// NewPrometheusHistogramExt creates and returns new PrometheusHistogram in s
// with the given name and upperBounds.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned histogram is safe to use from concurrent goroutines.
func (s *Set) NewPrometheusHistogramExt(
	name string,
	upperBounds []float64,
	labels ...Label,
) *metrics.PrometheusHistogram {
	return s.set.NewPrometheusHistogramExt(buildName(name, s.labels, labels), upperBounds)
}

// GetOrCreatePrometheusHistogram returns registered prometheus histogram in s
// with the given name or creates new histogram if s doesn't contain histogram
// with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned histogram is safe to use from concurrent goroutines.
//
// Performance tip: prefer NewPrometheusHistogram instead of GetOrCreatePrometheusHistogram.
func (s *Set) GetOrCreatePrometheusHistogram(name string, labels ...Label) *metrics.PrometheusHistogram {
	return s.set.GetOrCreatePrometheusHistogram(buildName(name, s.labels, labels))
}

// GetOrCreatePrometheusHistogramExt returns registered prometheus histogram in
// s with the given name or creates new histogram if s doesn't contain
// histogram with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned histogram is safe to use from concurrent goroutines.
//
// Performance tip: prefer NewPrometheusHistogramExt instead of GetOrCreatePrometheusHistogramExt.
func (s *Set) GetOrCreatePrometheusHistogramExt(
	name string,
	upperBounds []float64,
	labels ...Label,
) *metrics.PrometheusHistogram {
	return s.set.GetOrCreatePrometheusHistogramExt(buildName(name, s.labels, labels), upperBounds)
}

// NewCounter registers and returns new counter with the given name in the s.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned counter is safe to use from concurrent goroutines.
func (s *Set) NewCounter(name string, labels ...Label) *metrics.Counter {
	return s.set.NewCounter(buildName(name, s.labels, labels))
}

// GetOrCreateCounter returns registered counter in s with the given name
// or creates new counter if s doesn't contain counter with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned counter is safe to use from concurrent goroutines.
//
// Performance tip: prefer NewCounter instead of GetOrCreateCounter.
func (s *Set) GetOrCreateCounter(name string, labels ...Label) *metrics.Counter {
	return s.set.GetOrCreateCounter(buildName(name, s.labels, labels))
}

// NewFloatCounter registers and returns new FloatCounter with the given name in the s.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned FloatCounter is safe to use from concurrent goroutines.
func (s *Set) NewFloatCounter(name string, labels ...Label) *metrics.FloatCounter {
	return s.set.NewFloatCounter(buildName(name, s.labels, labels))
}

// GetOrCreateFloatCounter returns registered FloatCounter in s with the given name
// or creates new FloatCounter if s doesn't contain FloatCounter with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned FloatCounter is safe to use from concurrent goroutines.
//
// Performance tip: prefer NewFloatCounter instead of GetOrCreateFloatCounter.
func (s *Set) GetOrCreateFloatCounter(name string, labels ...Label) *metrics.FloatCounter {
	return s.set.GetOrCreateFloatCounter(buildName(name, s.labels, labels))
}

// NewGauge registers and returns gauge with the given name in s, which calls f
// to obtain gauge value.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// f must be safe for concurrent calls.
//
// The returned gauge is safe to use from concurrent goroutines.
func (s *Set) NewGauge(name string, labels ...Label) *metrics.Gauge {
	return s.set.NewGauge(buildName(name, s.labels, labels), nil)
}

// GetOrCreateGauge returns registered gauge with the given name in s
// or creates new gauge if s doesn't contain gauge with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned gauge is safe to use from concurrent goroutines.
//
// Performance tip: prefer NewGauge instead of GetOrCreateGauge.
func (s *Set) GetOrCreateGauge(name string, labels ...Label) *metrics.Gauge {
	return s.set.GetOrCreateGauge(buildName(name, s.labels, labels), nil)
}

// NewSummary creates and returns new summary with the given name in s.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned summary is safe to use from concurrent goroutines.
func (s *Set) NewSummary(name string, labels ...Label) *metrics.Summary {
	return s.set.NewSummary(buildName(name, s.labels, labels))
}

// NewSummaryExt creates and returns new summary in s with the given name,
// window and quantiles.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned summary is safe to use from concurrent goroutines.
func (s *Set) NewSummaryExt(name string, window time.Duration, quantiles []float64, labels ...Label) *metrics.Summary {
	return s.set.NewSummaryExt(buildName(name, s.labels, labels), window, quantiles)
}

// GetOrCreateSummary returns registered summary with the given name in s
// or creates new summary if s doesn't contain summary with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned summary is safe to use from concurrent goroutines.
//
// Performance tip: prefer NewSummary instead of GetOrCreateSummary.
func (s *Set) GetOrCreateSummary(name string, labels ...Label) *metrics.Summary {
	return s.set.GetOrCreateSummary(buildName(name, s.labels, labels))
}

// GetOrCreateSummaryExt returns registered summary with the given name,
// window and quantiles in s or creates new summary if s doesn't
// contain summary with the given name.
//
// name must be valid Prometheus-compatible metric with possible labels.
// For instance,
//
//   - foo
//   - foo{bar="baz"}
//   - foo{bar="baz",aaa="b"}
//
// The returned summary is safe to use from concurrent goroutines.
//
// Performance tip: prefer NewSummaryExt instead of GetOrCreateSummaryExt.
func (s *Set) GetOrCreateSummaryExt(
	name string,
	window time.Duration,
	quantiles []float64,
	labels ...Label,
) *metrics.Summary {
	return s.set.GetOrCreateSummaryExt(buildName(name, s.labels, labels), window, quantiles)
}

func buildName(name string, labels ...[]Label) string {
	sb := strings.Builder{}

	l := 2 + len(name)
	var count int
	for _, ls := range labels {
		for _, label := range ls {
			l += label.length
			count++
		}
	}

	sb.Grow(l)

	sb.WriteString(name)
	var i int
	for _, ls := range labels {
		for _, label := range ls {
			i++
			if i == 1 {
				sb.WriteString("{")
			}
			sb.WriteString(label.name)
			sb.WriteString(`="`)
			sb.WriteString(label.value)
			sb.WriteString(`"`)
			if i == count {
				sb.WriteString("}")
				return sb.String()
			}
			sb.WriteString(",")
		}
	}
	return sb.String()
}
