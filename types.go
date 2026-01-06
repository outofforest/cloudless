package cloudless

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
	Base                 Base
	KernelPackage        Package
	KernelModulePackages []Package
	KernelModules        []string
}

// EFI represents source of FI loader.
type EFI struct {
	Version string
	Hash    string
}

// Base represents source of base OS filesystem.
type Base struct {
	URL  string
	Hash string
}

// Package represents RPM package to take files from.
type Package struct {
	Name    string
	Version string
	Hash    string
}
