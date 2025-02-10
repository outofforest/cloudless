package build

import (
	"context"

	"github.com/outofforest/build/v2/pkg/tools"
	"github.com/outofforest/build/v2/pkg/types"
	"github.com/outofforest/tools/pkg/tools/golang"
)

const initBinPath = "bin/init"

func buildInit(ctx context.Context, deps types.DepsFunc) error {
	deps(golang.EnsureGo, golang.Generate)

	return golang.Build(ctx, deps, golang.BuildConfig{
		Platform:      tools.PlatformLocal,
		PackagePath:   "cmd/init",
		BinOutputPath: initBinPath,
	})
}
