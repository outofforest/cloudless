package cloudless

// Config is the configuration of loader builder.
type Config struct {
	InitBinPath string
	EFIPath     string
	Distro      DistroConfig
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
