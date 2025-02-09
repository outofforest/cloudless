package build

var config = Config{
	Base: Base{
		URL:    "https://github.com/fedora-cloud/docker-brew-fedora/raw/54e85723288471bb9dc81bc5cfed807635f93818/x86_64/fedora-20250119.tar", //nolint:lll
		SHA256: "3b8a25c27f4773557aee851f75beba17d910c968671c2771a105ce7c7a40e3ec",
	},

	// https://packages.fedoraproject.org
	KernelPackage: Package{
		Name:    "kernel-core",
		Version: "6.12.7-200.fc41.x86_64",
		SHA256:  "f8b5314f347a8533f864a322a15209c36f164f78a9419a382143a145bcaf1c6f",
	},
	KernelModulePackages: []Package{
		{
			Name:    "kernel-modules-core",
			Version: "6.12.7-200.fc41.x86_64",
			SHA256:  "791f222e27395c571319c93eb17cbf391bfbd8955557478e5301152834b3b662",
		},
		{
			Name:    "kernel-modules",
			Version: "6.12.7-200.fc41.x86_64",
			SHA256:  "d5d9603ec1bf97b01c30f98992dda9993b8a7cd4f885eb5b732064ec2f6b4936",
		},
	},
	KernelModules: []string{
		"tun",
		"kvm-intel",
		"virtio-net",
		"vhost-net",
		"virtio-scsi",
		"bridge",
		"veth",
		"nft-masq",
		"nft-nat",
		"nft-fib-ipv4",
		"nft-ct",
		"nft-chain-nat",
	},
	InitBinPath: initBinPath,
}
