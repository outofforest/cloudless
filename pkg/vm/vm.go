package vm

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"text/template"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/parse"
)

var (
	//go:embed vm.tmpl.xml
	vmDef string

	vmDefTmpl = lo.Must(template.New("vm").Parse(vmDef))
)

// Config represents vm configuration.
type Config struct {
	Networks []NetworkConfig
}

// NetworkConfig represents vm's network configuration.
type NetworkConfig struct {
	BridgeName    string
	InterfaceName string
	MAC           net.HardwareAddr
}

// Configurator defines function setting the vm configuration.
type Configurator func(vm *Config)

// New creates vm.
func New(name string, cores, memory uint64, configurators ...Configurator) host.Configurator {
	var vm Config

	for _, configurator := range configurators {
		configurator(&vm)
	}

	return cloudless.Join(
		cloudless.CreateInitramfs(),
		cloudless.StartVirtServices(),
		cloudless.KernelModules(
			kernel.Module{
				Name:   "kvm-intel",
				Params: "nested=Y",
			},
			kernel.Module{Name: "tun"},
		),
		cloudless.AllocateHugePages(memory),
		cloudless.Prepare(func(_ context.Context) error {
			vmUUID := uuid.New()

			filePath := fmt.Sprintf("/etc/libvirt/qemu/%s.xml", name)
			f, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			data := struct {
				UUID     uuid.UUID
				Name     string
				Cores    uint64
				VCPUs    uint64
				Memory   uint64
				Kernel   string
				Initrd   string
				Networks []NetworkConfig
			}{
				UUID:     vmUUID,
				Name:     name,
				Cores:    cores,
				VCPUs:    2 * cores,
				Memory:   memory,
				Kernel:   "/boot/vmlinuz",
				Initrd:   "/boot/initramfs",
				Networks: vm.Networks,
			}

			if err := vmDefTmpl.Execute(f, data); err != nil {
				return errors.WithStack(err)
			}

			return errors.WithStack(os.Link(filePath,
				filepath.Join(filepath.Dir(filePath), "autostart", filepath.Base(filePath))))
		}),
	)
}

// Network adds network to the config.
func Network(bridgeName, ifaceName, mac string) Configurator {
	return func(vm *Config) {
		vm.Networks = append(vm.Networks, NetworkConfig{
			BridgeName:    bridgeName,
			InterfaceName: ifaceName,
			MAC:           parse.MAC(mac),
		})
	}
}
