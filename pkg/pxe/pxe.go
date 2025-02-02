package pxe

import (
	"context"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/pxe/dhcp6"
	"github.com/outofforest/cloudless/pkg/pxe/tftp"
	"github.com/outofforest/parallel"
)

// Service returns PXE service.
func Service(efiDevPath string) host.Configurator {
	return cloudless.Join(
		cloudless.Firewall(
			firewall.OpenV6UDPPort(dhcp6.Port),
			firewall.OpenV6UDPPort(tftp.Port),
		),
		cloudless.Service("pxe", parallel.Fail, func(ctx context.Context) error {
			return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
				spawn("dhcp6", parallel.Fail, dhcp6.Run)
				spawn("tftp", parallel.Fail, tftp.NewRun(efiDevPath))
				return nil
			})
		}),
	)
}
