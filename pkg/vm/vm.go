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
	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/parse"
	"github.com/outofforest/cloudless/pkg/virt"
)

var (
	//go:embed vm.tmpl.xml
	vmDef     string
	vmDefTmpl = lo.Must(template.New("vm").Parse(vmDef))

	//go:embed pool.xml
	poolDef string

	//go:embed volume.tmpl.xml
	volumeDef     string
	volumeDefTmpl = lo.Must(template.New("volume").Parse(volumeDef))
)

// Config represents vm configuration.
type Config struct {
	Name          string
	EFIDiskPath   string
	KernelPath    string
	InitramfsPath string
	Volumes       []VolumeConfig
	Networks      []NetworkConfig
	Bridges       []NetworkConfig
}

// VolumeConfig represents vm's volume configuration.
type VolumeConfig struct {
	Name   string
	Device string
	Size   uint64
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
	Name          string
	Cores         uint64
	VCPUs         uint64
	Memory        uint64
	KernelPath    string
	InitramfsPath string
	EFIDiskPath   string
	Volumes       []VolumeConfig
	Networks      []NetworkConfig
	Bridges       []NetworkConfig
}

// New creates vm.
func New(name string, cores, memory uint64, configurators ...Configurator) host.Configurator {
	vm := Config{
		Name:          name,
		KernelPath:    "/boot/vmlinuz",
		InitramfsPath: "/boot/initramfs",
	}

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
			filePath := fmt.Sprintf("/etc/libvirt/qemu/%s.xml", name)
			f, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return errors.WithStack(err)
			}
			defer f.Close()

			data := spec{
				Volumes:       vm.Volumes,
				Name:          vm.Name,
				Cores:         cores,
				VCPUs:         2 * cores,
				Memory:        memory,
				InitramfsPath: vm.InitramfsPath,
				EFIDiskPath:   vm.EFIDiskPath,
				Networks:      vm.Networks,
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
func Spec(name string, cores, memory uint64, configurators ...Configurator) cloudless.SpecSource {
	vm := Config{
		Name: name,
	}

	for _, configurator := range configurators {
		configurator(&vm)
	}

	return func(l *libvirt.Libvirt) error {
		if err := createVolumes(l, vm.Volumes); err != nil {
			return err
		}

		data := spec{
			Name:          vm.Name,
			Cores:         cores,
			VCPUs:         2 * cores,
			Memory:        memory,
			KernelPath:    vm.KernelPath,
			InitramfsPath: vm.InitramfsPath,
			EFIDiskPath:   vm.EFIDiskPath,
			Volumes:       vm.Volumes,
			Networks:      vm.Networks,
			Bridges:       vm.Bridges,
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

// KernelBoot configures VM to boot from kernel and initramfs.
func KernelBoot(kernelPath, initramfsPath string) Configurator {
	return func(vm *Config) {
		vm.EFIDiskPath = ""
		vm.KernelPath = kernelPath
		vm.InitramfsPath = initramfsPath
	}
}

// EFIBoot configures VM to boot from EFI partition.
func EFIBoot(efiDiskPath string) Configurator {
	return func(vm *Config) {
		vm.KernelPath = ""
		vm.InitramfsPath = ""
		vm.EFIDiskPath = efiDiskPath
	}
}

// Disk adds disk to the config.
func Disk(name, device string, size uint64) Configurator {
	const gb = 1024 * 1024 * 1024
	return func(vm *Config) {
		vm.Volumes = append(vm.Volumes, VolumeConfig{
			Name:   vm.Name + "-" + name,
			Device: device,
			Size:   size * gb,
		})
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

func createVolumes(l *libvirt.Libvirt, volumes []VolumeConfig) error {
	if len(volumes) == 0 {
		return nil
	}

	pool, err := l.StoragePoolLookupByName(virt.StoragePoolName)
	switch {
	case err == nil:
	case virt.IsError(err, libvirt.ErrNoStoragePool):
		pool, err = l.StoragePoolDefineXML(poolDef, 0)
		if err != nil {
			return errors.WithStack(err)
		}
		if err := l.StoragePoolBuild(pool, libvirt.StoragePoolBuildNew); err != nil {
			return errors.WithStack(err)
		}
	default:
		return errors.WithStack(err)
	}

	active, err := l.StoragePoolIsActive(pool)
	if err != nil {
		return errors.WithStack(err)
	}
	if active == 0 {
		if err := l.StoragePoolCreate(pool, libvirt.StoragePoolCreateNormal); err != nil {
			return errors.WithStack(err)
		}
	}

	for _, v := range volumes {
		_, err := l.StorageVolLookupByName(pool, v.Name)
		switch {
		case err == nil:
			continue
		case virt.IsError(err, libvirt.ErrNoStorageVol):
		default:
			return errors.WithStack(err)
		}

		buf := &bytes.Buffer{}
		if err := volumeDefTmpl.Execute(buf, v); err != nil {
			return errors.WithStack(err)
		}

		if _, err := l.StorageVolCreateXML(pool, buf.String(), 0); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}
