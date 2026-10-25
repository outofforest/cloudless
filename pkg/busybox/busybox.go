package busybox

import (
	"github.com/outofforest/cloudless/pkg"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/host"
)

// Install installs busybox inside container.
func Install() host.Configurator {
	return container.InstallImage(pkg.BusyboxDockerImage)
}
