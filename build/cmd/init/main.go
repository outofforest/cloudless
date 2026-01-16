package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/dev"
)

var deployment = Deployment(
	ImmediateKernelModules(DefaultKernelModules...),
	dev.Boxes("10.255.0.2:9000"),

	HostService,
)

func main() {
	Main(deployment...)
}
