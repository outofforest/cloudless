package bridge

import (
	"context"
	"net"
	"strings"

	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/parse"
)

// Config represents network configuration.
type Config struct {
	Name string
	MAC  net.HardwareAddr
	IPs  []net.IPNet
}

// New creates new bridge.
func New(name, mac string, ips ...string) host.Configurator {
	config := Config{
		Name: name,
		MAC:  parse.MAC(mac),
	}
	for _, ip := range ips {
		if strings.Contains(ip, ".") {
			config.IPs = append(config.IPs, parse.IPNet4(ip))
		} else {
			config.IPs = append(config.IPs, parse.IPNet6(ip))
		}
	}

	return cloudless.Join(
		cloudless.KernelModules(kernel.Module{
			Name: "tun",
		}),
		cloudless.Firewall(
			firewall.Forward(config.Name),
			firewall.Masquerade(config.Name),
		),
		cloudless.EnableIPForwarding(),
		cloudless.Prepare(func(_ context.Context) error {
			return createBridge(config)
		}),
	)
}

func createBridge(config Config) error {
	bridge := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:         config.Name,
			HardwareAddr: config.MAC,
		},
	}

	if err := netlink.LinkAdd(bridge); err != nil {
		return errors.WithStack(err)
	}

	for _, ip := range config.IPs {
		if err := netlink.AddrAdd(bridge, &netlink.Addr{IPNet: &ip}); err != nil {
			return errors.WithStack(err)
		}
	}

	if err := netlink.LinkSetUp(bridge); err != nil {
		return errors.WithStack(err)
	}

	return nil
}
