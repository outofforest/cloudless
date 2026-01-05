package build

import (
	"context"

	"github.com/outofforest/build/v2/pkg/tools"
	"github.com/outofforest/build/v2/pkg/types"
	"github.com/outofforest/cloudless"
	"github.com/outofforest/tools/pkg/tools/golang"
)

const (
	initBinPath   = "bin/init"
	moduleVersion = "6.17.12-300.fc43.x86_64"
)

var config = cloudless.Config{
	InitBinPath: initBinPath,
	EFIPath:     "bin/efi.img",
	Distro: cloudless.DistroConfig{
		EFI: cloudless.EFI{
			Version: "v1.0.0",
			Hash:    "sha256:661c4dc913c366aef7645e9f381a2357baf9ae125a8587637da0e281e669730f",
		},
		Base: cloudless.Base{
			URL:  "https://github.com/fedora-cloud/docker-brew-fedora/raw/refs/heads/43/x86_64/fedora-20260104.tar",
			Hash: "sha256:12cee601b760e21f3a8aacfb11dbe926255a414ef3cc4b66682df74413c1bab1",
		},
		KernelPackage: cloudless.Package{
			Name:    "kernel-core",
			Version: moduleVersion,
			Hash:    "sha256:a37e6912e51108c8983ea1f0f23f4e1cbf07380d73f22ea1d3099ce431438062",
		},
		KernelModulePackages: []cloudless.Package{
			{
				Name:    "kernel-modules-core",
				Version: moduleVersion,
				Hash:    "sha256:51a340f9fd9d537c4a7ee9174a3ce88c2d1732353ff912ed4ee08093d98fe399",
			},
			{
				Name:    "kernel-modules",
				Version: moduleVersion,
				Hash:    "sha256:faca4a5eed0afb6f507a4422d42c4aae8eaa7d0244bfbb000914310388b26063",
			},
		},
		KernelModules: []string{
			"vfat",
			"tun",
			"kvm-intel",
			"virtio-net",
			"virtio-scsi",
			"bridge",
			"veth",
			"8021q",
			"vhost-net",
			"nft-masq",
			"nft-nat",
			"nft-fib-ipv4",
			"nft-ct",
			"nft-chain-nat",
		},
	},
}

func buildEFI(ctx context.Context, deps types.DepsFunc) error {
	deps(buildInit)

	return cloudless.BuildEFI(ctx, deps, config)
}

func buildInit(ctx context.Context, deps types.DepsFunc) error {
	deps(golang.EnsureGo, golang.Generate)

	return golang.Build(ctx, deps, golang.BuildConfig{
		Platform:      tools.PlatformLocal,
		PackagePath:   "build/cmd/init",
		BinOutputPath: initBinPath,
	})
}
