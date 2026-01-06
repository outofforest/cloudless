package vm

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"text/template"

	"github.com/digitalocean/go-libvirt"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/dev"
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
	EFIDiskPath string
	Networks    []NetworkConfig
	Bridges     []NetworkConfig
}

// NetworkConfig represents vm's network configuration.
type NetworkConfig struct {
	SourceName    string
	InterfaceName string
	MAC           net.HardwareAddr
}

// Configurator defines function setting the vm configuration.
type Configurator func(vm *Config)

type spec struct {
	UUID        uuid.UUID
	Name        string
	Cores       uint64
	VCPUs       uint64
	Memory      uint64
	Kernel      string
	Initrd      string
	EFIDiskPath string
	Networks    []NetworkConfig
	Bridges     []NetworkConfig
}

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
			kernel.Module{Name: "vhost-net"},
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

			data := spec{
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

// Spec defines dev spec of vm.
func Spec(name string, cores, memory uint64, configurators ...Configurator) dev.SpecSource {
	var vm Config

	for _, configurator := range configurators {
		configurator(&vm)
	}

	return func(l *libvirt.Libvirt) error {
		vmUUID := uuid.New()

		data := spec{
			UUID:        vmUUID,
			Name:        name,
			Cores:       cores,
			VCPUs:       2 * cores,
			Memory:      memory,
			EFIDiskPath: vm.EFIDiskPath,
			Networks:    vm.Networks,
			Bridges:     vm.Bridges,
		}

		buf := &bytes.Buffer{}
		if err := vmDefTmpl.Execute(buf, data); err != nil {
			return errors.WithStack(err)
		}

		vm, err := l.DomainDefineXML(buf.String())
		if err != nil {
			return errors.WithStack(err)
		}
		return errors.WithStack(l.DomainCreate(vm))
	}
}

// EFIBoot configures VM to boot from EFI partition.
func EFIBoot(efiDiskPath string) Configurator {
	return func(vm *Config) {
		vm.EFIDiskPath = efiDiskPath
	}
}

// Network adds network to the config.
func Network(networkName, ifaceName, mac string) Configurator {
	return func(vm *Config) {
		vm.Networks = append(vm.Networks, NetworkConfig{
			SourceName:    networkName,
			InterfaceName: ifaceName,
			MAC:           parse.MAC(mac),
		})
	}
}

// Bridge adds bridged network to the config.
func Bridge(bridgeName, ifaceName, mac string) Configurator {
	return func(vm *Config) {
		vm.Bridges = append(vm.Bridges, NetworkConfig{
			SourceName:    bridgeName,
			InterfaceName: ifaceName,
			MAC:           parse.MAC(mac),
		})
	}
}
