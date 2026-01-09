package main

import . "github.com/outofforest/cloudless" //nolint:staticcheck

var deployment = Deployment(
	ImmediateKernelModules(DefaultKernelModules...),
	DNS(DefaultDNS...),

	HostService,
	HostMonitoring,
)

func main() {
	Main(deployment...)
}
