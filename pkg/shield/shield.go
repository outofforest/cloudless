package shield

import (
	"github.com/google/nftables"
	"github.com/pkg/errors"

	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/host/firewall/rules"
	"github.com/outofforest/cloudless/pkg/parse"
)

// Forward allows connections to be made from input to output interface.
func Forward(inIface, outIface string) host.Configurator {
	return func(c *host.Configuration) error {
		c.RequireIPForwarding()
		c.AddFirewallRules(func(chains firewall.Chains) ([]*nftables.Rule, error) {
			return []*nftables.Rule{
				{
					Chain: chains.V4FilterForward,
					Exprs: rules.Expressions(
						rules.IncomingInterface(inIface),
						rules.OutgoingInterface(outIface),
						rules.Accept(),
					),
				},
				{
					Chain: chains.V4FilterForward,
					Exprs: rules.Expressions(
						rules.IncomingInterface(outIface),
						rules.OutgoingInterface(inIface),
						rules.ConnectionEstablished(),
						rules.Accept(),
					),
				},
			}, nil
		})
		return nil
	}
}

// Masquerade masquerades connections made from input interface through output interface.
func Masquerade(inIface, outIface string) host.Configurator {
	return func(c *host.Configuration) error {
		c.RequireIPForwarding()
		c.AddFirewallRules(func(chains firewall.Chains) ([]*nftables.Rule, error) {
			return []*nftables.Rule{
				{
					Chain: chains.V4FilterForward,
					Exprs: rules.Expressions(
						rules.IncomingInterface(inIface),
						rules.OutgoingInterface(outIface),
						rules.Accept(),
					),
				},
				{
					Chain: chains.V4FilterForward,
					Exprs: rules.Expressions(
						rules.IncomingInterface(outIface),
						rules.OutgoingInterface(inIface),
						rules.ConnectionEstablished(),
						rules.Accept(),
					),
				},
				{
					Chain: chains.V4NATPostrouting,
					Exprs: rules.Expressions(
						rules.IncomingInterface(inIface),
						rules.OutgoingInterface(outIface),
						rules.Masquerade(),
					),
				},
			}, nil
		})
		return nil
	}
}

// Source modifies the source IP of packets coming from input interface and going through output interface.
func Source(inIface, outIface, externalIP string) host.Configurator {
	externalIPParsed := parse.IP4(externalIP)
	return func(c *host.Configuration) error {
		c.RequireIPForwarding()
		c.AddFirewallRules(func(chains firewall.Chains) ([]*nftables.Rule, error) {
			return []*nftables.Rule{
				{
					Chain: chains.V4FilterForward,
					Exprs: rules.Expressions(
						rules.IncomingInterface(inIface),
						rules.OutgoingInterface(outIface),
						rules.Accept(),
					),
				},
				{
					Chain: chains.V4FilterForward,
					Exprs: rules.Expressions(
						rules.IncomingInterface(outIface),
						rules.OutgoingInterface(inIface),
						rules.ConnectionEstablished(),
						rules.Accept(),
					),
				},
				{
					Chain: chains.V4NATPostrouting,
					Exprs: rules.Expressions(
						rules.IncomingInterface(inIface),
						rules.OutgoingInterface(outIface),
						rules.SourceNAT(externalIPParsed),
					),
				},
			}, nil
		})
		return nil
	}
}

// Expose exposes internal port on external address.
func Expose(proto string,
	externalIP string,
	externalPort uint16,
	internalIP string,
	internalPort uint16,
) host.Configurator {
	externalIPParsed := parse.IP4(externalIP)
	internalIPParsed := parse.IP4(internalIP)
	return func(c *host.Configuration) error {
		c.RequireIPForwarding()
		c.AddFirewallRules(func(chains firewall.Chains) ([]*nftables.Rule, error) {
			return []*nftables.Rule{
				{
					Chain: chains.V4FilterForward,
					Exprs: rules.Expressions(
						rules.Protocol(proto),
						rules.DestinationAddress(internalIPParsed),
						rules.DestinationPort(internalPort),
						rules.Accept(),
					),
				},
				{
					Chain: chains.V4FilterForward,
					Exprs: rules.Expressions(
						rules.Protocol(proto),
						rules.ConnectionEstablished(),
						rules.SourceAddress(internalIPParsed),
						rules.SourcePort(internalPort),
						rules.Accept(),
					),
				},
				{
					Chain: chains.V4NATPrerouting,
					Exprs: rules.Expressions(
						rules.Protocol(proto),
						rules.DestinationAddress(externalIPParsed),
						rules.DestinationPort(externalPort),
						rules.DestinationNAT(internalIPParsed, internalPort),
					),
				},
			}, nil
		})
		return nil
	}
}

var validProtos = map[string]struct{}{
	"udp4": {},
	"tcp4": {},
	"udp6": {},
	"tcp6": {},
}

// Open accepts connections coming to port.
func Open(proto, iface string, port uint16) host.Configurator {
	if _, exists := validProtos[proto]; !exists {
		panic(errors.Errorf("invalid protocol: %s", proto))
	}

	return func(c *host.Configuration) error {
		c.AddFirewallRules(func(chains firewall.Chains) ([]*nftables.Rule, error) {
			chain := chains.V4FilterInput
			if proto[len(proto)-1] == '6' {
				chain = chains.V6FilterInput
			}

			return []*nftables.Rule{
				{
					Chain: chain,
					Exprs: rules.Expressions(
						rules.Protocol(proto[:len(proto)-1]),
						rules.IncomingInterface(iface),
						rules.DestinationPort(port),
						rules.Accept(),
					),
				},
			}, nil
		})
		return nil
	}
}
