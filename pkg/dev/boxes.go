package dev

import (
	"github.com/digitalocean/go-libvirt"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/vm"
)

// Boxes adds dev boxes to the environment.
func Boxes() host.Configurator {
	return cloudless.Join(
		monitoringBox,
		devBox,
	)
}

// VMs adds dev vms to the environment.
func VMs(bootConfigurator vm.Configurator) cloudless.SpecSource {
	vms := []cloudless.SpecSource{
		vm.Spec("cloudless-monitoring", 4, 2,
			bootConfigurator,
			vm.Network("cloudless-igw", "vigw1", "fc:ff:ff:fe:00:01"),
			vm.Disk("monitoring", "vda", 20),
		),
		vm.Spec("cloudless-dev", 2, 1,
			bootConfigurator,
			vm.Network("cloudless-igw", "vigw2", "fc:ff:ff:ff:00:01"),
			vm.Disk("dev", "vda", 20),
		),
	}

	return func(l *libvirt.Libvirt) error {
		for _, vm := range vms {
			if err := vm(l); err != nil {
				return err
			}
		}
		return nil
	}
}
