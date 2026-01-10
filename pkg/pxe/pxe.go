package pxe

import (
	"context"
	"path/filepath"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/pxe/dhcp6"
	"github.com/outofforest/cloudless/pkg/pxe/tftp"
	"github.com/outofforest/parallel"
)

// Service returns PXE service.
func Service(efiDev string) host.Configurator {
	return cloudless.Join(
		// VFAT is loaded to enable EFI partition mounting, to update the bootloader.
		cloudless.KernelModules(kernel.Module{Name: "vfat"}),
		cloudless.Service("pxe", parallel.Fail, func(ctx context.Context) error {
			return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
				spawn("dhcp6", parallel.Fail, dhcp6.Run)
				spawn("tftp", parallel.Fail, tftp.NewRun(filepath.Join("/dev", efiDev)))
				return nil
			})
		}),
	)
}
