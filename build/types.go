package build

// Config is the configuration of loader builder.
type Config struct {
	InitBinPath string
	Distro      DistroConfig
}

// DistroConfig is the configuration of distro builder.
type DistroConfig struct {
	Base                 Base
	KernelPackage        Package
	KernelModulePackages []Package
	KernelModules        []string
}

// Base represents source of base OS filesystem.
type Base struct {
	URL    string
	SHA256 string
}

// Package represents RPM package to take files from.
type Package struct {
	Name    string
	Version string
	SHA256  string
}
