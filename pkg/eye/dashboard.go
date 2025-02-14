package eye

import (
	_ "embed"

	grafanatypes "github.com/outofforest/cloudless/pkg/grafana/types"
)

// Dashboard is the dashboard providing host parameters.
//
//go:embed dashboard.json
var Dashboard grafanatypes.Dashboard
