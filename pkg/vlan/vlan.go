package vlan

import (
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/parse"
)

// Configurator defines function configuring vlan interface.
type Configurator func(c *host.VLANConfig)

// New defines vlan interface.
func New(ifaceName, parent string, configurators ...Configurator) host.Configurator {
	config := host.VLANConfig{
		Name:       ifaceName,
		ParentName: parent,
	}

	for _, configurator := range configurators {
		configurator(&config)
	}

	return func(c *host.Configuration) error {
		c.RequireKernelModules(
			kernel.Module{Name: "8021q"},
		)
		c.AddVLANs(config)
		return nil
	}
}

// IPs sets IP addresses on vlan interface.
func IPs(ips ...string) Configurator {
	return func(c *host.VLANConfig) {
		for _, ip := range ips {
			c.IPs = append(c.IPs, parse.IPNet(ip))
		}
	}
}
