package cloudless

import (
	"bytes"
	"net"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/vishvananda/netlink"

	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/parse"
	"github.com/outofforest/parallel"
)

var (
	// DefaultKernelModules is the reasonable list of kernel modules providing networking and storage.
	DefaultKernelModules = []kernel.Module{
		// Networking.
		{Name: "virtio-net"},

		// NFTables.
		{Name: "nft-masq"},
		{Name: "nft-nat"},
		{Name: "nft-fib-ipv4"},
		{Name: "nft-ct"},
		{Name: "nft-chain-nat"},

		// Storage.
		{Name: "virtio-scsi"},
	}

	// DefaultDNS is the list of default DNS servers.
	DefaultDNS = []string{
		"1.1.1.1",
		"8.8.8.8",
	}
)

// Deployment converts inlined spec into a slice.
func Deployment(configurators ...host.Configurator) []host.Configurator {
	return configurators
}

// Box defines system configuration.
func Box(hostname string, configurators ...host.Configurator) host.Configurator {
	return func(c *host.Configuration) error {
		cfg, mergeFn := host.NewSubconfiguration(c)
		cfg.SetHostname(hostname)

		var notThisHost bool
		for _, configurator := range configurators {
			err := configurator(cfg)
			switch {
			case err == nil:
			case errors.Is(err, host.ErrNotThisHost):
				notThisHost = true
			default:
				return err
			}
		}

		// This is done like this to register all the required packages in the repo and don't skip anything.
		if notThisHost {
			return nil
		}

		mergeFn()

		return host.ErrHostFound
	}
}

// BoxFunc is the function configuring box.
type BoxFunc func(hostname string, configurators ...host.Configurator) host.Configurator

// BoxFactory returns box factory with preconfigured components.
func BoxFactory(configurators ...host.Configurator) BoxFunc {
	return func(hostname string, configurators2 ...host.Configurator) host.Configurator {
		return Box(hostname, append(append([]host.Configurator{}, configurators...), configurators2...)...)
	}
}

// ExtendBoxFactory extends box factory with another sets of configurators.
func ExtendBoxFactory(boxFunc BoxFunc, configurators ...host.Configurator) BoxFunc {
	return func(hostname string, configurators2 ...host.Configurator) host.Configurator {
		return boxFunc(hostname, append(append([]host.Configurator{}, configurators...), configurators2...)...)
	}
}

// Join combines many configurator into a single one.
func Join(configurators ...host.Configurator) host.Configurator {
	return func(c *host.Configuration) error {
		var hostErr error
		for _, configurator := range configurators {
			err := configurator(c)
			switch {
			case err == nil:
			case errors.Is(err, host.ErrHostFound):
				if hostErr != nil {
					return errors.New("host matches many configurations")
				}
				hostErr = err
			default:
				return err
			}
		}

		return hostErr
	}
}

// If combines configurators if condition is true.
func If(condition bool, configurators ...host.Configurator) host.Configurator {
	if !condition {
		return func(c *host.Configuration) error {
			return nil
		}
	}
	return Join(configurators...)
}

// Configuration provides host configuration.
func Configuration(cfg *host.SealedConfiguration) host.Configurator {
	return func(c *host.Configuration) error {
		*cfg = c.Sealed()
		return nil
	}
}

// IsHost verifies that image is run on host.
func IsHost() host.Configurator {
	return func(c *host.Configuration) error {
		c.HostOnly()
		return nil
	}
}

// IsContainer verifies that image is run inside container.
func IsContainer() host.Configurator {
	return func(c *host.Configuration) error {
		c.ContainerOnly()
		return nil
	}
}

// Gateway defines gateway.
func Gateway(gateway string) host.Configurator {
	ip := parse.IP(gateway)
	return func(c *host.Configuration) error {
		c.SetGateway(ip)
		return nil
	}
}

// Route defines static route.
func Route(destination, gateway string) host.Configurator {
	destinationParsed := parse.IPNet(destination)
	gatewayParsed := parse.IP(gateway)
	return func(c *host.Configuration) error {
		c.AddRoutes(host.Route{
			Destination: destinationParsed,
			Gateway:     gatewayParsed,
		})
		return nil
	}
}

// InterfaceConfigurator is a type alias for functions that configure a specific network interface.
type InterfaceConfigurator func(c *host.InterfaceConfig)

// Network defines network.
func Network(mac, ifaceName string, configurators ...InterfaceConfigurator) host.Configurator {
	config := host.InterfaceConfig{
		Name: ifaceName,
		MAC:  parse.MAC(mac),
	}

	for _, configurator := range configurators {
		configurator(&config)
	}

	return func(c *host.Configuration) error {
		links, err := netlink.LinkList()
		if err != nil {
			return errors.WithStack(err)
		}

		for _, l := range links {
			if bytes.Equal(config.MAC, l.Attrs().HardwareAddr) {
				c.AddNetworks(config)
				return nil
			}
		}

		return host.ErrNotThisHost
	}
}

// Bridge defines bridge interface.
func Bridge(ifaceName, mac string, configurators ...InterfaceConfigurator) host.Configurator {
	config := host.InterfaceConfig{
		Name: ifaceName,
		MAC:  parse.MAC(mac),
	}

	for _, configurator := range configurators {
		configurator(&config)
	}

	return func(c *host.Configuration) error {
		c.RequireKernelModules(
			kernel.Module{Name: "bridge"},
		)
		c.AddBridges(config)
		return nil
	}
}

// IPs configures the IP addresses for a network interface.
func IPs(ips ...string) InterfaceConfigurator {
	return func(c *host.InterfaceConfig) {
		for _, ip := range ips {
			c.IPs = append(c.IPs, parse.IPNet(ip))
		}
	}
}

// Master sets the master interface name for a network interface.
func Master(bridge string) InterfaceConfigurator {
	return func(c *host.InterfaceConfig) {
		c.MasterName = bridge
	}
}

// KernelModules defines kernel modules to load.
func KernelModules(modules ...kernel.Module) host.Configurator {
	return func(c *host.Configuration) error {
		c.RequireKernelModules(modules...)
		return nil
	}
}

// ImmediateKernelModules load kernel modules immediately.
func ImmediateKernelModules(modules ...kernel.Module) host.Configurator {
	return func(c *host.Configuration) error {
		if c.IsContainer() {
			return nil
		}
		return host.ConfigureKernelModules(modules)
	}
}

// DNS defines DNS servers.
func DNS(dns ...string) host.Configurator {
	ips := make([]net.IP, 0, len(dns))
	for _, d := range dns {
		ips = append(ips, parse.IP4(d))
	}

	return func(c *host.Configuration) error {
		c.AddDNSes(ips...)
		return nil
	}
}

// Mount defines mount.
func Mount(source, target string, writable bool) host.Configurator {
	return func(c *host.Configuration) error {
		if c.IsContainer() {
			target = filepath.Join(".", target)
		}
		c.AddMount(source, target, writable)
		return nil
	}
}

// CreateInitramfs creates initramfs file.
func CreateInitramfs() host.Configurator {
	return func(c *host.Configuration) error {
		c.RequireInitramfs()
		return nil
	}
}

// StartVirtServices starts virtualization services.
func StartVirtServices() host.Configurator {
	return func(c *host.Configuration) error {
		c.RequireVirt()
		return nil
	}
}

// AllocateHugePages allocates huge pages.
func AllocateHugePages(hugePages uint64) host.Configurator {
	return func(c *host.Configuration) error {
		c.AddHugePages(hugePages)
		return nil
	}
}

// RequireContainers requires container images to be downloaded and cached.
func RequireContainers(images ...string) host.Configurator {
	return func(c *host.Configuration) error {
		c.RequireContainers(images...)
		return nil
	}
}

// Prune adds decision maker if root directory should be pruned.
func Prune(prunes ...host.PruneFn) host.Configurator {
	return func(c *host.Configuration) error {
		c.Prune(prunes...)
		return nil
	}
}

// Prepare runs system preparation function.
func Prepare(prepares ...host.PrepareFn) host.Configurator {
	return func(c *host.Configuration) error {
		c.Prepare(prepares...)
		return nil
	}
}

// Service starts service.
func Service(name string, onExit parallel.OnExit, task parallel.Task) host.Configurator {
	return func(c *host.Configuration) error {
		c.StartServices(host.ServiceConfig{
			Name:   name,
			OnExit: onExit,
			TaskFn: task,
		})
		return nil
	}
}

// Metrics registers prometheus metric gatherers.
func Metrics(gatherers ...prometheus.Gatherer) host.Configurator {
	return func(c *host.Configuration) error {
		c.RegisterMetrics(gatherers...)
		return nil
	}
}
