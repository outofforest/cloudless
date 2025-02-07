package firewall

import (
	"github.com/google/nftables"

	"github.com/outofforest/cloudless/pkg/host/firewall/rules"
)

// Masquerade masquerades traffic.
func Masquerade(iface string) RuleSource {
	return func(chains Chains) ([]*nftables.Rule, error) {
		return []*nftables.Rule{
			{
				Chain: chains.V4NATPostrouting,
				Exprs: rules.Expressions(
					rules.IncomingInterface(iface),
					rules.Masquerade(),
				),
			},
		}, nil
	}
}
