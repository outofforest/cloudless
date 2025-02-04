package host

import (
	_ "embed"

	grafanatypes "github.com/outofforest/cloudless/pkg/grafana/types"
)

// DashboardBoxes is the dashboard providing basic information about running boxes.
//
//go:embed dashboard.json
var DashboardBoxes grafanatypes.Dashboard
