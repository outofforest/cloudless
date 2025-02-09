package build

import (
	"context"

	"github.com/outofforest/build/v2/pkg/types"
)

// Commands is a definition of commands available in build system.
var Commands = map[string]types.Command{
	"build": {Fn: func(ctx context.Context, deps types.DepsFunc) error {
		deps(buildInit)

		return Loader(ctx, deps, config)
	}, Description: "Builds loader"},
}
