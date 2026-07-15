package ingress

import (
	"context"
	"net"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/cloudless/pkg/wave"
)

const (
	// PortHTTP is the port for http traffic.
	PortHTTP = 80

	// PortHTTPS is the port for https traffic.
	PortHTTPS = 443
)

// Service returns ingress service.
func Service(waveConfig wave.Config, configurators ...Configurator) host.Configurator {
	return cloudless.Service("ingress", func(ctx context.Context) error {
		serviceConfig := ServiceConfig{
			Config: Config{
				WaveConfig: waveConfig,
				Endpoints:  map[EndpointID]EndpointConfig{},
				Targets:    map[EndpointID][]TargetConfig{},
			},
			Endpoints: map[EndpointID]ServiceEndpointConfig{},
		}

		for _, configurator := range configurators {
			configurator(&serviceConfig)
		}
		lss := map[string]net.Listener{}
		defer func() {
			for _, ls := range lss {
				_ = ls.Close()
			}
		}()

		for eID, e := range serviceConfig.Endpoints {
			eConfig := e.Config

			plainLss, err := createListeners(ctx, e.PlainBindings, lss)
			if err != nil {
				return err
			}

			tlsLss, err := createListeners(ctx, e.TLSBindings, lss)
			if err != nil {
				return err
			}

			eConfig.PlainListeners = plainLss
			eConfig.TLSListeners = tlsLss

			serviceConfig.Config.Endpoints[eID] = eConfig
		}

		return New(serviceConfig.Config).Run(ctx)
	})
}

func createListeners(
	ctx context.Context,
	bindings map[string]struct{},
	listeners map[string]net.Listener,
) ([]net.Listener, error) {
	lss := make([]net.Listener, 0, len(bindings))
	for bAddr := range bindings {
		ls := listeners[bAddr]
		if ls == nil {
			var err error
			ls, err = tnet.Listen(ctx, bAddr)
			if err != nil {
				return nil, err
			}
			listeners[bAddr] = ls
		}
		lss = append(lss, ls)
	}
	return lss, nil
}
