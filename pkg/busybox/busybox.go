package busybox

import (
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
)

const image = "busybox@sha256:e4749fb2291b57af91d8de04dd4664428b1f1cf49c257018a3153e722a6f21ae"

// Install installs busybox inside container.
func Install() host.Configurator {
	return container.InstallImage(image)
}
