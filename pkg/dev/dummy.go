package dev

import (
	"context"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/parallel"
)

// DummyService is used to keep box running without any service.
func DummyService() host.Configurator {
	return cloudless.Service("dummy", parallel.Exit, func(ctx context.Context) error {
		<-ctx.Done()
		return errors.WithStack(ctx.Err())
	})
}
