package cloudless

import "github.com/digitalocean/go-libvirt"

// Config is the configuration of loader builder.
type Config struct {
	Input  InputConfig
	Output OutputConfig
	Distro DistroConfig
}

// InputConfig stores paths to input files.
type InputConfig struct {
	InitBin string
}

// OutputConfig stores paths to output files.
type OutputConfig struct {
	EFI       string
	Kernel    string
	Initramfs string
}

// DistroConfig is the configuration of distro builder.
type DistroConfig struct {
	EFI                  EFI
	Base                 Resource
	KernelPackage        Resource
	KernelModulePackages []Resource
	BtrfsPackages        []Resource
	KernelModules        []string
}

// EFI represents source of FI loader.
type EFI struct {
	Version string
	Hash    string
}

// Resource represents downloadable resource.
type Resource struct {
	URL  string
	Hash string
}

// SpecSource defines a function installing libvirt objects.
type SpecSource func(l *libvirt.Libvirt) error
