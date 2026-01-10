package dns

import (
	"net"

	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/parse"
)

var defaultServers = []string{
	"1.1.1.1",
	"8.8.8.8",
}

// DNS defines DNS servers.
func DNS(dns ...string) host.Configurator {
	if len(dns) == 0 {
		dns = defaultServers
	}
	ips := make([]net.IP, 0, len(dns))
	for _, d := range dns {
		ips = append(ips, parse.IP4(d))
	}

	return func(c *host.Configuration) error {
		c.AddDNSes(ips...)
		return nil
	}
}
