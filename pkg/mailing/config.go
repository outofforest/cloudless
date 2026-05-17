package mailing

import (
	"context"
	"net"

	"github.com/pkg/errors"

	"github.com/outofforest/cloudless/pkg/wave"
)

// Config is the maling config.
type Config struct {
	Hostname string
	Resolver *net.Resolver
	Wave     wave.ClientConfig
}

// DNSConfig is the DNS config of mailing.
type DNSConfig struct {
	Servers []string
}

// NewDNSConfig creates new DNS config for mailing.
func NewDNSConfig(dnsServers ...string) DNSConfig {
	return DNSConfig{
		Servers: dnsServers,
	}
}

// NewConfig creates new mailing config.
func NewConfig(hostname string, waveConfig wave.ClientConfig, dnsConfig DNSConfig) Config {
	c := Config{
		Hostname: hostname,
		Wave:     waveConfig,
		Resolver: net.DefaultResolver,
	}

	if len(dnsConfig.Servers) > 0 {
		dialer := &net.Dialer{}
		c.Resolver = &net.Resolver{
			PreferGo: false,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, dnsConfig.Servers[0])
				return conn, errors.WithStack(err)
			},
		}
	}

	return c
}
