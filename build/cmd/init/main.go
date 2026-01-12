package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/dev"
)

var deployment = Deployment(
	ImmediateKernelModules(DefaultKernelModules...),
	dev.Boxes(),

	HostService,
)

func main() {
	Main(deployment...)
}
