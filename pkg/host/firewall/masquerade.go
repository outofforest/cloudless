package firewall

import (
	"github.com/google/nftables"

	"github.com/outofforest/cloudless/pkg/host/firewall/rules"
	"github.com/outofforest/cloudless/pkg/tnet"
)

// Masquerade masquerades traffic.
func Masquerade(iface string) RuleSource {
	return func(chains Chains) ([]*nftables.Rule, error) {
		defaultIface, err := tnet.DefaultIface()
		if err != nil {
			return nil, err
		}

		return []*nftables.Rule{
			{
				Chain: chains.V4FilterForward,
				Exprs: rules.Expressions(
					rules.IncomingInterface(iface),
					rules.OutgoingInterface(defaultIface),
					rules.Accept(),
				),
			},
			{
				Chain: chains.V4FilterForward,
				Exprs: rules.Expressions(
					rules.IncomingInterface(defaultIface),
					rules.OutgoingInterface(iface),
					rules.ConnectionEstablished(),
					rules.Accept(),
				),
			},
			{
				Chain: chains.V4NATPostrouting,
				Exprs: rules.Expressions(
					rules.IncomingInterface(iface),
					rules.OutgoingInterface(defaultIface),
					rules.Masquerade(),
				),
			},
		}, nil
	}
}
