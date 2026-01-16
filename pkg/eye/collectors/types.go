package collectors

import (
	"github.com/outofforest/cloudless/pkg/eye/metrics"
	"github.com/outofforest/parallel"
)

// CollectorFunc describes function of metric collector.
type CollectorFunc func() (string, *metrics.Set, parallel.Task)
