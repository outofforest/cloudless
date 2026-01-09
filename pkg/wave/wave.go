package wave

import (
	"context"
	"net"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/parallel"
	"github.com/outofforest/wave"
)

// Port is the port wave servers listens on.
const Port = 1024

// Address returns address of wave endpoint.
func Address(host string) string {
	return tnet.Join(host, Port)
}

// Service returns Wave service.
func Service(maxMessageSize uint64, configurators ...Configurator) host.Configurator {
	config := wave.ServerConfig{
		MaxMessageSize: maxMessageSize,
	}
	for _, configurator := range configurators {
		configurator(&config)
	}

	return cloudless.Service("wave", parallel.Fail, func(ctx context.Context) error {
		ls, err := net.ListenTCP("tcp4", &net.TCPAddr{
			IP:   net.IPv4zero,
			Port: Port,
		})
		if err != nil {
			return errors.WithStack(err)
		}
		defer ls.Close()

		return wave.RunServer(ctx, ls, config)
	})
}
