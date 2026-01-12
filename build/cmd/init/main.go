package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
)

var deployment = Deployment(
	ImmediateKernelModules(DefaultKernelModules...),
	HostService,
	HostMonitoring,
	HostDev,
)

func main() {
	Main(deployment...)
}
