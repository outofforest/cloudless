package build

const version = "6.13.1-200.fc41.x86_64"

var config = Config{
	InitBinPath: initBinPath,
	Distro: DistroConfig{
		Base: Base{
			URL:    "https://github.com/fedora-cloud/docker-brew-fedora/raw/54e85723288471bb9dc81bc5cfed807635f93818/x86_64/fedora-20250119.tar", //nolint:lll
			SHA256: "3b8a25c27f4773557aee851f75beba17d910c968671c2771a105ce7c7a40e3ec",
		},

		// https://packages.fedoraproject.org
		KernelPackage: Package{
			Name:    "kernel-core",
			Version: version,
			SHA256:  "1ae495e4fafc9efc2ecb4f13a73c87db12c75121b2aab07408c8e0ad69de9c4d",
		},
		KernelModulePackages: []Package{
			{
				Name:    "kernel-modules-core",
				Version: version,
				SHA256:  "87f687a9b3299c910b350f3c77fa0b5883e15d96b9a2049d5382a5b7c2b2d390",
			},
			{
				Name:    "kernel-modules",
				Version: version,
				SHA256:  "7a7a1580c1a418de7fef58f4403d5933e679a44612b1a29bf7a1c1c755f488aa",
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
			"nft-masq",
			"nft-nat",
			"nft-fib-ipv4",
			"nft-ct",
			"nft-chain-nat",
		},
	},
}
