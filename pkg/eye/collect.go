package eye

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/eye/collectors/cpu"
	"github.com/outofforest/cloudless/pkg/eye/collectors/disks"
	"github.com/outofforest/cloudless/pkg/eye/collectors/memory"
	"github.com/outofforest/cloudless/pkg/eye/collectors/mounts"
	"github.com/outofforest/cloudless/pkg/eye/collectors/network"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/parallel"
)

const collectInterval = time.Second

var collectors = []func() (string, prometheus.Gatherer, parallel.Task){
	cpu.New(collectInterval),
	memory.New(collectInterval),
	network.New(collectInterval),
	mounts.New(collectInterval),
	disks.New(collectInterval),
}

// CollectService returns new service collecting system metrics.
func CollectService() host.Configurator {
	gatherers := make([]prometheus.Gatherer, 0, len(collectors))
	tasks := make([]task, 0, len(collectors))
	for _, c := range collectors {
		n, g, t := c()
		gatherers = append(gatherers, g)
		tasks = append(tasks, task{
			Name: n,
			Task: t,
		})
	}

	return cloudless.Join(
		cloudless.Metrics(gatherers...),
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
