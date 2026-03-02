package ingress

import (
	"context"
	"net"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/tnet"
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
	return cloudless.Service("ingress", parallel.Fail, func(ctx context.Context) error {
		serviceConfig := ServiceConfig{
			Config: Config{
				Endpoints: map[EndpointID]EndpointConfig{},
				Targets:   map[EndpointID][]TargetConfig{},
			},
			Endpoints: map[EndpointID]ServiceEndpointConfig{},
		}

		for _, configurator := range configurators {
			configurator(&serviceConfig)
		}
		var lss []net.Listener
		defer func() {
			for _, ls := range lss {
				_ = ls.Close()
			}
		}()

		for eID, e := range serviceConfig.Endpoints {
			eConfig := e.Config

			plainLss, err := createListeners(ctx, e.PlainBindings)
			if err != nil {
				return err
			}
			lss = append(lss, plainLss...)

			tlsLss, err := createListeners(ctx, e.TLSBindings)
			if err != nil {
				return err
			}
			lss = append(lss, tlsLss...)

			eConfig.PlainListeners = plainLss
			eConfig.PlainListeners = tlsLss

			serviceConfig.Config.Endpoints[eID] = eConfig
		}

		return New(serviceConfig.Config).Run(ctx)
	})
}

func createListeners(ctx context.Context, bindings []string) ([]net.Listener, error) {
	lss := make([]net.Listener, 0, len(bindings))
	for _, bAddr := range bindings {
		ls, err := tnet.Listen(ctx, bAddr)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		lss = append(lss, ls)
	}
	return lss, nil
}
