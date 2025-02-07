package firewall

import (
	"github.com/google/nftables"

	"github.com/outofforest/cloudless/pkg/host/firewall/rules"
)

// ForwardTo accepts traffic forwarded to the interface.
func ForwardTo(iface string) RuleSource {
	return func(chains Chains) ([]*nftables.Rule, error) {
		return []*nftables.Rule{
			{
				Chain: chains.V4FilterForward,
				Exprs: rules.Expressions(
					rules.IncomingInterface(iface),
					rules.Accept(),
				),
			},
		}, nil
	}
}

// ForwardFrom accepts traffic forwarded from the interface.
func ForwardFrom(iface string) RuleSource {
	return func(chains Chains) ([]*nftables.Rule, error) {
		return []*nftables.Rule{
			{
				Chain: chains.V4FilterForward,
				Exprs: rules.Expressions(
					rules.OutgoingInterface(iface),
					rules.Accept(),
				),
			},
		}, nil
	}
}

// Forward enables forwarding connections outgoing from the interface.
func Forward(iface string) RuleSource {
	return func(chains Chains) ([]*nftables.Rule, error) {
		return []*nftables.Rule{
			{
				Chain: chains.V4FilterForward,
				Exprs: rules.Expressions(
					rules.OutgoingInterface(iface),
					rules.ConnectionEstablished(),
					rules.Accept(),
				),
			},
			{
				Chain: chains.V4FilterForward,
				Exprs: rules.Expressions(
					rules.IncomingInterface(iface),
					rules.Accept(),
				),
			},
		}, nil
	}
}
