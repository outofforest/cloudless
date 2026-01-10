package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/dns"
)

var deployment = Deployment(
	ImmediateKernelModules(DefaultKernelModules...),
	dns.DNS(),

	HostService,
	HostMonitoring,
	HostDev,
)

func main() {
	Main(deployment...)
}
