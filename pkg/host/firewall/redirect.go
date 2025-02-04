package firewall

import (
	"net"

	"github.com/google/nftables"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"

	"github.com/outofforest/cloudless/pkg/host/firewall/rules"
	"github.com/outofforest/cloudless/pkg/parse"
)

// RedirectV4TCPPort redirects TCPv4 port.
func RedirectV4TCPPort(externalIP string, externalPort uint16, internalIP string, internalPort uint16) RuleSource {
	externalIPParsed := parse.IP4(externalIP)
	internalIPParsed := parse.IP4(internalIP)
	return func(chains Chains) ([]*nftables.Rule, error) {
		iface, err := findInterfaceByIP(externalIPParsed)
		if err != nil {
			return nil, err
		}
		return []*nftables.Rule{
			// Redirecting requests from the host machine.
			{
				Chain: chains.V4NATOutput,
				Exprs: rules.Expressions(
					rules.Protocol("tcp"),
					rules.DestinationAddress(externalIPParsed),
					rules.DestinationPort(externalPort),
					rules.DestinationNAT(internalIPParsed, internalPort),
				),
			},

			// Redirecting requests coming from other interfaces.
			{
				Chain: chains.V4NATPostrouting,
				Exprs: rules.Expressions(
					rules.Protocol("tcp"),
					rules.NotIncomingInterface(iface),
					rules.DestinationAddress(internalIPParsed),
					rules.DestinationPort(internalPort),
					rules.Masquerade(),
				),
			},

			// Redirecting external requests.
			{
				Chain: chains.V4NATPrerouting,
				Exprs: rules.Expressions(
					rules.Protocol("tcp"),
					rules.DestinationAddress(externalIPParsed),
					rules.DestinationPort(externalPort),
					rules.DestinationNAT(internalIPParsed, internalPort),
				),
			},
		}, nil
	}
}

// RedirectV4UDPPort redirects UDPv4 port.
func RedirectV4UDPPort(externalIP string, externalPort uint16, internalIP string, internalPort uint16) RuleSource {
	externalIPParsed := parse.IP4(externalIP)
	internalIPParsed := parse.IP4(internalIP)
	return func(chains Chains) ([]*nftables.Rule, error) {
		return []*nftables.Rule{
			{
				Chain: chains.V4NATPrerouting,
				Exprs: rules.Expressions(
					rules.Protocol("udp"),
					rules.LocalDestinationAddress(),
					rules.DestinationAddress(externalIPParsed),
					rules.DestinationPort(externalPort),
					rules.DestinationNAT(internalIPParsed, internalPort),
				),
			},
		}, nil
	}
}

func findInterfaceByIP(ip net.IP) (string, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return "", errors.WithStack(err)
	}

	for _, l := range links {
		addrs, err := netlink.AddrList(l, netlink.FAMILY_V4)
		if err != nil {
			return "", errors.WithStack(err)
		}
		for _, addr := range addrs {
			if addr.IP.Equal(ip) {
				return l.Attrs().Name, nil
			}
		}
	}

	return "", errors.Errorf("no link for address %q", ip)
}
