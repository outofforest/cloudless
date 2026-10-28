package mailing

import (
	"context"
	"net"

	"github.com/pkg/errors"
)

// Config is the maling config.
type Config struct {
	Hostname string
	Resolver *net.Resolver
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
func NewConfig(hostname string, dnsConfig DNSConfig) Config {
	c := Config{
		Hostname: hostname,
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
