package ingress

import (
	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/parallel"
)

const (
	// PortHTTP is the port for http traffic.
	PortHTTP = 80

	// PortHTTPS is the port for https traffic.
	PortHTTPS = 443
)

// Service returns ingress service.
func Service(config Config) host.Configurator {
	return cloudless.Join(
		cloudless.Firewall(
			firewall.OpenV4TCPPort(PortHTTP),
			firewall.OpenV4TCPPort(PortHTTPS),
		),
		cloudless.Service("ingress", parallel.Fail, New(config).Run),
	)
}
