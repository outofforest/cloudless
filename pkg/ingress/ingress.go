package ingress

import (
	"context"

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
func Service(configurators ...Configurator) host.Configurator {
	return cloudless.Join(
		cloudless.Firewall(
			firewall.OpenV4TCPPort(PortHTTP),
			firewall.OpenV4TCPPort(PortHTTPS),
		),
		cloudless.Service("ingress", parallel.Fail, func(ctx context.Context) error {
			config := Config{
				Endpoints: map[EndpointID]EndpointConfig{},
				Targets:   map[EndpointID][]TargetConfig{},
			}

			for _, configurator := range configurators {
				configurator(&config)
			}

			return New(config).Run(ctx)
		}),
	)
}
