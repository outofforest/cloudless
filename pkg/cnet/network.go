package cnet

import (
	"context"
	"net"
	"strings"

	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/parse"
)

// Config represents network configuration.
type Config struct {
	BridgeName string
	IP4        net.IPNet
	IP6        net.IPNet
}

// Configurator defines function setting the network configuration.
type Configurator func(n *Config)

// NAT creates NATed network.
func NAT(name string, configurators ...Configurator) host.Configurator {
	config := Config{
		BridgeName: BridgeName(name),
	}
	for _, configurator := range configurators {
		configurator(&config)
	}

	return cloudless.Join(
		cloudless.Firewall(
			firewall.Masquerade(config.BridgeName),
		),
		cloudless.EnableIPForwarding(),
		cloudless.Prepare(func(_ context.Context) error {
			return createBridge(config)
		}),
	)
}

// IP4 configures network's IPv4 address on the host.
func IP4(ip string) Configurator {
	return func(n *Config) {
		n.IP4 = parse.IPNet4(ip)
	}
}

// IP6 configures network's IPv6 address on the host.
func IP6(ip string) Configurator {
	return func(n *Config) {
		n.IP6 = parse.IPNet6(ip)
	}
}

// BridgeName returns bridge interface name for network.
func BridgeName(network string) string {
	bridgeName := "c" + strings.ToLower(network)
	if len(bridgeName) > 15 {
		bridgeName = bridgeName[:15]
	}

	return bridgeName
}

func createBridge(config Config) error {
	bridge := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: config.BridgeName,
		},
	}

	if err := netlink.LinkAdd(bridge); err != nil {
		return errors.WithStack(err)
	}

	if config.IP4.IP != nil {
		if err := netlink.AddrAdd(bridge, &netlink.Addr{IPNet: &config.IP4}); err != nil {
			return errors.WithStack(err)
		}
	}
	if config.IP6.IP != nil {
		if err := netlink.AddrAdd(bridge, &netlink.Addr{IPNet: &config.IP6}); err != nil {
			return errors.WithStack(err)
		}
	}

	if err := netlink.LinkSetUp(bridge); err != nil {
		return errors.WithStack(err)
	}

	return nil
}
