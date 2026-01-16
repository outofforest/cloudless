package metrics

import (
	"strings"
	"time"
)

// N produces name of the metric.
func N(namespace, subsystem, name string) string {
	if name == "" {
		return ""
	}

	sb := strings.Builder{}

	l := 2 + len(namespace) + len(subsystem) + len(name)
	sb.Grow(l)

	sb.WriteString(namespace)
	sb.WriteString("_")
	sb.WriteString(subsystem)
	sb.WriteString("_")
	sb.WriteString(name)

	return sb.String()
}

// Label defines the metric label.
type Label struct {
	length int
	name   string
	value  string
}

// L produces label for metric.
func L(name, value string) Label {
	return Label{
		length: 4 + len(name) + len(value),
		name:   name,
		value:  value,
	}
}

// Time turns time into float64 accepted by metric.
func Time(t time.Time) float64 {
	return float64(t.UnixNano()) / 1_000_000.0
}
