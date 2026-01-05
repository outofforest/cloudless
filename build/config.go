package build

const version = "6.17.12-300.fc43.x86_64"

var config = Config{
	InitBinPath: initBinPath,
	Distro: DistroConfig{
		Base: Base{
			URL:    "https://github.com/fedora-cloud/docker-brew-fedora/raw/refs/heads/43/x86_64/fedora-20260104.tar", //nolint:lll
			SHA256: "12cee601b760e21f3a8aacfb11dbe926255a414ef3cc4b66682df74413c1bab1",
		},

		// https://packages.fedoraproject.org
		KernelPackage: Package{
			Name:    "kernel-core",
			Version: version,
			SHA256:  "a37e6912e51108c8983ea1f0f23f4e1cbf07380d73f22ea1d3099ce431438062",
		},
		KernelModulePackages: []Package{
			{
				Name:    "kernel-modules-core",
				Version: version,
				SHA256:  "51a340f9fd9d537c4a7ee9174a3ce88c2d1732353ff912ed4ee08093d98fe399",
			},
			{
				Name:    "kernel-modules",
				Version: version,
				SHA256:  "faca4a5eed0afb6f507a4422d42c4aae8eaa7d0244bfbb000914310388b26063",
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
