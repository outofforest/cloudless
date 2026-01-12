package build

import (
	"context"
	"os"
	"path/filepath"

	"github.com/samber/lo"

	"github.com/outofforest/build/v2/pkg/tools"
	"github.com/outofforest/build/v2/pkg/types"
	"github.com/outofforest/cloudless"
	"github.com/outofforest/cloudless/pkg/vm"
	"github.com/outofforest/cloudless/pkg/vnet"
	"github.com/outofforest/tools/pkg/tools/golang"
)

// dnf download --url --arch=x86_64 kernel-core kernel-modules-core kernel-modules btrfs-progs e2fsprogs-libs lzo

var config = cloudless.Config{
	Input: cloudless.InputConfig{
		InitBin: "bin/init",
	},
	Output: cloudless.OutputConfig{
		EFI:       "bin/efi.img",
		Kernel:    "bin/vmlinuz",
		Initramfs: "bin/initramfs.img",
	},
	Distro: cloudless.DistroConfig{
		EFI: cloudless.EFI{
			Version: "v1.0.0",
			Hash:    "sha256:661c4dc913c366aef7645e9f381a2357baf9ae125a8587637da0e281e669730f",
		},
		Base: cloudless.Resource{
			URL:  "https://github.com/fedora-cloud/docker-brew-fedora/raw/refs/heads/43/x86_64/fedora-20260104.tar",
			Hash: "sha256:12cee601b760e21f3a8aacfb11dbe926255a414ef3cc4b66682df74413c1bab1",
		},

		// kernel-core
		KernelPackage: cloudless.Resource{
			//nolint:lll
			URL:  "https://mirrors.xtom.ee/fedora/updates/43/Everything/x86_64/Packages/k/kernel-core-6.18.3-200.fc43.x86_64.rpm",
			Hash: "sha256:9ee926a81441bc8b829c0dff9be087a3d0ed0668566bfde16a8f4a0d77a52fe8",
		},
		KernelModulePackages: []cloudless.Resource{
			// kernel-modules-core
			{
				//nolint:lll
				URL:  "https://mirrors.xtom.ee/fedora/updates/43/Everything/x86_64/Packages/k/kernel-modules-core-6.18.3-200.fc43.x86_64.rpm",
				Hash: "sha256:485b09b847391687a003ece4916421f4948e8f2e6b9e78abcbc3f54df1599045",
			},

			// kernel-modules
			{
				//nolint:lll
				URL:  "https://mirrors.xtom.ee/fedora/updates/43/Everything/x86_64/Packages/k/kernel-modules-6.18.3-200.fc43.x86_64.rpm",
				Hash: "sha256:cb4a5718cfbf0163cedc80a5f32a69a4f45e63f4b9e2280210ed5141412354f5",
			},
		},
		BtrfsPackages: []cloudless.Resource{
			// btrfs-progs
			{
				URL:  "https://mirrors.xtom.ee/fedora/updates/43/Everything/x86_64/Packages/b/btrfs-progs-6.17.1-1.fc43.x86_64.rpm",
				Hash: "sha256:1486db4fec1b295d351a6d715a70d2a5083282276c55cf7269b05a249759564b",
			},

			// e2fsprogs-libs
			{
				//nolint:lll
				URL:  "https://fedora.ip-connect.vn.ua/linux/releases/43/Everything/x86_64/os/Packages/e/e2fsprogs-libs-1.47.3-2.fc43.x86_64.rpm",
				Hash: "sha256:5e0d2f95049fdb27dde5220d9236d773e876be8dafce7fbe11e4bce7a6146bf1",
			},

			// lzo
			{
				//nolint:lll
				URL:  "https://fedora.ip-connect.vn.ua/linux/releases/43/Everything/x86_64/os/Packages/l/lzo-2.10-15.fc43.x86_64.rpm",
				Hash: "sha256:f894657345bb319c13bd99133ecd1d340bef4257a47ced15a5d20a85e98c3ffd",
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

const libvirtAddr = "tcp://10.0.0.1:16509"

func startKernel(ctx context.Context, deps types.DepsFunc) error {
	deps(stop, buildKernel)

	return start(vm.KernelBoot(
		hostPath(config.Output.Kernel),
		hostPath(config.Output.Initramfs),
	))
}

func startEFI(ctx context.Context, deps types.DepsFunc) error {
	deps(stop, buildEFI)

	return start(vm.EFIBoot(hostPath(config.Output.EFI)))
}

func start(bootConfigurator vm.Configurator) error {
	return cloudless.Start(libvirtAddr,
		vnet.NAT("cloudless-igw", "02:00:00:00:00:01", vnet.IPs("10.255.0.1/24")),
		vm.Spec("cloudless-service", 4, 2,
			bootConfigurator,
			vm.Network("cloudless-igw", "vigw0", "02:00:00:00:00:02"),
			vm.Disk("service", "vda", 20),
		),
		vm.Spec("cloudless-monitoring", 4, 2,
			bootConfigurator,
			vm.Network("cloudless-igw", "vigw1", "fc:ff:ff:fe:00:01"),
			vm.Disk("monitoring", "vda", 20),
		),
		vm.Spec("cloudless-dev", 2, 1,
			bootConfigurator,
			vm.Network("cloudless-igw", "vigw2", "fc:ff:ff:ff:00:01"),
			vm.Disk("dev", "vda", 20),
		),
	)
}

func stop(ctx context.Context, deps types.DepsFunc) error {
	return cloudless.Stop(ctx, libvirtAddr)
}

func destroy(ctx context.Context, deps types.DepsFunc) error {
	return cloudless.Destroy(ctx, libvirtAddr)
}

func verify(ctx context.Context, deps types.DepsFunc) error {
	return cloudless.Verify(ctx, config)
}

func hostPath(path string) string {
	return filepath.Join(
		"/tank/vms/vm-go/home-wojciech",
		lo.Must(filepath.Rel(lo.Must(os.UserHomeDir()), lo.Must(filepath.Abs(lo.Must(filepath.EvalSymlinks(path)))))),
	)
}

func buildKernel(ctx context.Context, deps types.DepsFunc) error {
	deps(buildInit)

	if err := cloudless.BuildKernel(ctx, config); err != nil {
		return err
	}
	return cloudless.BuildInitramfs(ctx, config)
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
		BinOutputPath: config.Input.InitBin,
	})
}
