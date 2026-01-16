package eye

import (
	"context"
	"time"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/eye/collectors"
	"github.com/outofforest/cloudless/pkg/eye/collectors/cpu"
	"github.com/outofforest/cloudless/pkg/eye/collectors/disks"
	"github.com/outofforest/cloudless/pkg/eye/collectors/memory"
	"github.com/outofforest/cloudless/pkg/eye/collectors/mounts"
	"github.com/outofforest/cloudless/pkg/eye/collectors/network"
	"github.com/outofforest/cloudless/pkg/eye/metrics"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/parallel"
)

const collectInterval = time.Second

var cs = []collectors.CollectorFunc{
	cpu.New(collectInterval),
	memory.New(collectInterval),
	network.New(collectInterval),
	mounts.New(collectInterval),
	disks.New(collectInterval),
}

// SystemMonitor returns new service collecting system metrics.
func SystemMonitor() host.Configurator {
	sets := make([]*metrics.Set, 0, len(cs))
	tasks := make([]task, 0, len(cs))
	for _, c := range cs {
		n, set, t := c()
		sets = append(sets, set)
		tasks = append(tasks, task{
			Name: n,
			Task: t,
		})
	}

	return cloudless.Join(
		cloudless.Metrics(sets...),
		cloudless.Service("eye", parallel.Fail, func(ctx context.Context) error {
			return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
				for _, t := range tasks {
					spawn(t.Name, parallel.Fail, t.Task)
				}
				return nil
			})
		}),
	)
}

type task struct {
	Name string
	Task parallel.Task
}
