package host

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cavaliergopher/cpio"
	"github.com/digitalocean/go-libvirt"
	"github.com/digitalocean/go-libvirt/socket/dialers"
	"github.com/google/nftables"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap"

	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/host/zombie"
	"github.com/outofforest/cloudless/pkg/kernel"
	"github.com/outofforest/cloudless/pkg/mount"
	"github.com/outofforest/cloudless/pkg/tcontext"
	"github.com/outofforest/libexec"
	"github.com/outofforest/logger"
	"github.com/outofforest/logger/remote"
	"github.com/outofforest/parallel"
)

const (
	// ContainerEnvVar is used to set container name.
	ContainerEnvVar = "CLOUDLESS_CONTAINER"

	filesystem = "btrfs"
	// https://btrfs.readthedocs.io/en/latest/Administration.html#btrfs-specific-mount-options
	btrfsOptions = "commit=1,flushoncommit,usebackuproot"

	qemuSocket = "/var/run/libvirt/virtqemud-sock"
)

var (
	// ErrPowerOff means that host should be powered off.
	ErrPowerOff = errors.New("power off requested")

	// ErrReboot means that host should be rebooted.
	ErrReboot = errors.New("reboot requested")

	// ErrNotThisHost is an indicator that spec is for different host.
	ErrNotThisHost = errors.New("not this host")

	// ErrHostFound is an indicator that this is the host matching the spec.
	ErrHostFound = errors.New("host found")

	virtPackages = []string{
		"libvirt-daemon-kvm",
		"qemu-kvm",
		"netcat",
	}

	//go:embed cloudless.repo
	cloudlessRepo []byte
)

// InterfaceConfig contains network interface configuration.
type InterfaceConfig struct {
	Name       string
	MasterName string
	MAC        net.HardwareAddr
	IPs        []net.IPNet
}

// VLANConfig contains vlan interface configuration.
type VLANConfig struct {
	Name       string
	ParentName string
	VLANID     int
	IPs        []net.IPNet
}

// ServiceConfig contains service configuration.
type ServiceConfig struct {
	Name   string
	OnExit parallel.OnExit
	TaskFn parallel.Task
}

func newPackageRepo() *packageRepo {
	return &packageRepo{
		packages: map[string]struct{}{},
	}
}

type packageRepo struct {
	packages map[string]struct{}
}

func (pr *packageRepo) Packages() []string {
	packages := make([]string, 0, len(pr.packages))
	for pkg := range pr.packages {
		packages = append(packages, pkg)
	}

	sort.Strings(packages)
	return packages
}

func (pr *packageRepo) Register(packages []string) {
	for _, pkg := range packages {
		pr.packages[pkg] = struct{}{}
	}
}

func newContainerImagesRepo() *containerImagesRepo {
	return &containerImagesRepo{
		images: map[string]struct{}{},
	}
}

type containerImagesRepo struct {
	images map[string]struct{}
}

func (cir *containerImagesRepo) ContainerImages() []string {
	images := make([]string, 0, len(cir.images))
	for pkg := range cir.images {
		images = append(images, pkg)
	}

	sort.Strings(images)
	return images
}

func (cir *containerImagesRepo) Register(images []string) {
	for _, image := range images {
		cir.images[image] = struct{}{}
	}
}

// PrepareFn is the function type used to register functions preparing host.
type PrepareFn func(ctx context.Context) error

// SealedConfiguration exposes information collected by the configurators.
type SealedConfiguration interface {
	IsContainer() bool
	MetricGatherer() prometheus.Gatherer
	Packages() []string
	ContainerImages() []string
	Hostname() string
	ContainerMirrors() []string
}

type mountConfig struct {
	Source   string
	Target   string
	Writable bool
}

type logLabels struct {
	Box string `json:"box"`
}

// Route defines static route.
type Route struct {
	Destination net.IPNet
	Gateway     net.IP
}

// Configuration allows service to configure the required host settings.
type Configuration struct {
	isContainer         bool
	topConfig           *Configuration
	pkgRepo             *packageRepo
	containerImagesRepo *containerImagesRepo
	remoteLoggingConfig remote.Config[logLabels]
	metricGatherers     prometheus.Gatherers

	requireIPForwarding bool
	requireInitramfs    bool
	requireVirt         bool
	kernelModules       []kernel.Module
	packages            []string
	hostname            string
	gateway             net.IP
	routes              []Route
	dnses               []net.IP
	yumMirrors          []string
	containerMirrors    []string
	networks            []InterfaceConfig
	bridges             []InterfaceConfig
	vlans               []VLANConfig
	firewall            []firewall.RuleSource
	hugePages           uint64
	prepare             []PrepareFn
	services            []ServiceConfig
	mounts              []mountConfig
}

// NewSubconfiguration creates subconfiguration.
func NewSubconfiguration(c *Configuration) (*Configuration, func()) {
	c2 := &Configuration{
		topConfig:           c,
		pkgRepo:             c.pkgRepo,
		containerImagesRepo: c.containerImagesRepo,
	}
	return c2, func() {
		if c2.remoteLoggingConfig.URL != "" {
			c.RemoteLogging(c2.remoteLoggingConfig.URL)
		}
		c.RegisterMetrics(c2.metricGatherers...)
		if c2.requireIPForwarding {
			c.RequireIPForwarding()
		}
		if c2.requireInitramfs {
			c.RequireInitramfs()
		}
		if c2.requireVirt {
			c.RequireVirt()
		}
		c.RequireKernelModules(c2.kernelModules...)
		c.RequirePackages(c2.packages...)
		c.SetHostname(c2.hostname)
		if c2.gateway != nil {
			c.SetGateway(c2.gateway)
		}
		c.AddRoutes(c2.routes...)
		c.AddDNSes(c2.dnses...)
		c.AddYumMirrors(c2.yumMirrors...)
		c.AddContainerMirrors(c2.containerMirrors...)
		c.AddNetworks(c2.networks...)
		c.AddBridges(c2.bridges...)
		c.AddVLANs(c2.vlans...)
		c.AddFirewallRules(c2.firewall...)
		c.AddHugePages(c2.hugePages)
		c.mounts = append(c.mounts, c2.mounts...)
		c.Prepare(c2.prepare...)
		c.StartServices(c2.services...)
	}
}

// Sealed gives access to information collected by configurators.
func (c *Configuration) Sealed() SealedConfiguration {
	return c.topConfig
}

// IsContainer informs if configurator is executed in the context of container.
func (c *Configuration) IsContainer() bool {
	return c.topConfig.isContainer
}

// RemoteLogging configures remote logging.
func (c *Configuration) RemoteLogging(lokiURL string) {
	c.remoteLoggingConfig.URL = lokiURL
}

// MetricGatherer returns prometheus metric gatherer.
func (c *Configuration) MetricGatherer() prometheus.Gatherer {
	return c.topConfig.metricGatherers
}

// RegisterMetrics registers prometheus metric gatherers.
func (c *Configuration) RegisterMetrics(gatherers ...prometheus.Gatherer) {
	c.metricGatherers = append(c.metricGatherers, gatherers...)
}

// RequireIPForwarding is called if host requires IP forwarding to be enabled.
func (c *Configuration) RequireIPForwarding() {
	c.requireIPForwarding = true
}

// RequireInitramfs is called if host requires initramfs to be generated.
func (c *Configuration) RequireInitramfs() {
	c.requireInitramfs = true
}

// RequireVirt is called if host requires virtualization services.
func (c *Configuration) RequireVirt() {
	c.requireVirt = true
	c.pkgRepo.Register(virtPackages)
}

// RequireKernelModules is called to load kernel modules.
func (c *Configuration) RequireKernelModules(kernelModules ...kernel.Module) {
	c.kernelModules = append(c.kernelModules, kernelModules...)
}

// Packages returns the list of packages configured for any host.
func (c *Configuration) Packages() []string {
	return c.pkgRepo.Packages()
}

// RequirePackages is called to install packages.
func (c *Configuration) RequirePackages(packages ...string) {
	c.pkgRepo.Register(packages)
	c.packages = append(c.packages, packages...)
}

// ContainerImages returns the list of container images required by any host.
func (c *Configuration) ContainerImages() []string {
	return c.containerImagesRepo.ContainerImages()
}

// RequireContainers is called to download container images.
func (c *Configuration) RequireContainers(images ...string) {
	c.containerImagesRepo.Register(images)
}

// Hostname returns hostname.
func (c *Configuration) Hostname() string {
	return c.hostname
}

// SetHostname sets hostname.
func (c *Configuration) SetHostname(hostname string) {
	c.hostname = hostname
}

// SetGateway sets gateway.
func (c *Configuration) SetGateway(gateway net.IP) {
	c.gateway = gateway
}

// AddRoutes adds static routes.
func (c *Configuration) AddRoutes(routes ...Route) {
	c.routes = append(c.routes, routes...)
}

// AddDNSes adds DNS servers.
func (c *Configuration) AddDNSes(dnses ...net.IP) {
	c.dnses = append(c.dnses, dnses...)
}

// AddYumMirrors adds package repository mirrors.
func (c *Configuration) AddYumMirrors(mirrors ...string) {
	c.yumMirrors = append(c.yumMirrors, mirrors...)
}

// ContainerMirrors returns list of container image mirrors.
func (c *Configuration) ContainerMirrors() []string {
	return c.topConfig.containerMirrors
}

// AddContainerMirrors adds container image mirrors.
func (c *Configuration) AddContainerMirrors(mirrors ...string) {
	c.containerMirrors = append(c.containerMirrors, mirrors...)
}

// AddNetworks configures networks.
func (c *Configuration) AddNetworks(networks ...InterfaceConfig) {
	c.networks = append(c.networks, networks...)
}

// AddBridges configures bridges.
func (c *Configuration) AddBridges(bridges ...InterfaceConfig) {
	c.bridges = append(c.bridges, bridges...)
}

// AddVLANs configures vlans.
func (c *Configuration) AddVLANs(vlans ...VLANConfig) {
	c.vlans = append(c.vlans, vlans...)
}

// AddFirewallRules add firewall rules.
func (c *Configuration) AddFirewallRules(sources ...firewall.RuleSource) {
	c.firewall = append(c.firewall, sources...)
}

// AddHugePages adds number of hugepages to be allocated.
func (c *Configuration) AddHugePages(hugePages uint64) {
	c.hugePages += hugePages
}

// AddMount adds mount.
func (c *Configuration) AddMount(source, target string, writable bool) {
	c.mounts = append(c.mounts, mountConfig{
		Source:   source,
		Target:   target,
		Writable: writable,
	})
}

// Prepare adds prepare function to be called.
func (c *Configuration) Prepare(prepares ...PrepareFn) {
	c.prepare = append(c.prepare, prepares...)
}

// StartServices configures services to be started on host.
func (c *Configuration) StartServices(services ...ServiceConfig) {
	c.services = append(c.services, services...)
}

// Configurator is the function called to collect host configuration.
type Configurator func(c *Configuration) error

// Run runs host.
//
//nolint:gocyclo
func Run(ctx context.Context, configurators ...Configurator) error {
	boxMetrics, boxMetricGatherer := newMetrics()
	cfg := &Configuration{
		isContainer:         isContainer(),
		pkgRepo:             newPackageRepo(),
		containerImagesRepo: newContainerImagesRepo(),
		metricGatherers: prometheus.Gatherers{
			boxMetricGatherer,
		},
	}
	cfg.topConfig = cfg

	if cfg.isContainer {
		if err := mount.ContainerRootPrepare(); err != nil {
			return err
		}

		// By closing stdin parent process signals that everything is prepared.
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			return errors.WithStack(err)
		}
	} else {
		if err := mount.HostRoot(); err != nil {
			return err
		}
	}

	var hostFound bool
	for _, c := range configurators {
		err := c(cfg)
		switch {
		case err == nil:
		case errors.Is(err, ErrHostFound):
			if hostFound {
				return errors.New("host matches many configurations")
			}
			hostFound = true
		default:
			return err
		}
	}

	if !hostFound {
		return errors.New("host does not match the configuration")
	}

	var sendLogsTask parallel.Task
	if cfg.remoteLoggingConfig.URL != "" {
		ctx, sendLogsTask = remote.WithRemote(ctx, cfg.remoteLoggingConfig)
	}
	ctx = logger.With(ctx, zap.String("box", cfg.hostname))

	boxMetrics.BoxStarted()

	err := parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		if sendLogsTask != nil {
			spawn("logSender", parallel.Fail, sendLogsTask)
		}
		spawn("", parallel.Fail, func(ctx context.Context) (retErr error) {
			defer func() {
				if err := unmount(); err != nil {
					retErr = err
				}
			}()

			//nolint:nestif
			if !cfg.isContainer {
				if cfg.requireVirt {
					setupVirt(cfg)
				}

				if cfg.requireInitramfs {
					if err := buildInitramfs(); err != nil {
						return err
					}
				}
				if err := removeOldRoot(); err != nil {
					return err
				}
				if err := ConfigureKernelModules(cfg.kernelModules); err != nil {
					return err
				}
			}

			if err := configureMounts(cfg.mounts); err != nil {
				return err
			}

			if cfg.isContainer {
				if err := mount.ContainerRoot(); err != nil {
					return err
				}
			}

			if err := configureDNS(cfg.dnses); err != nil {
				return err
			}
			if err := configureIPv6(); err != nil {
				return err
			}
			if err := configureEnv(cfg.hostname); err != nil {
				return err
			}
			if err := configureHostname(cfg.hostname); err != nil {
				return err
			}
			if err := configureNetworks(cfg.networks); err != nil {
				return err
			}
			if err := configureBridges(cfg.bridges); err != nil {
				return err
			}
			if err := configureVLANs(cfg.vlans); err != nil {
				return err
			}
			if err := configureFirewall(cfg.firewall); err != nil {
				return err
			}
			if err := configureGateway(cfg.gateway); err != nil {
				return err
			}
			if err := configureRoutes(cfg.routes); err != nil {
				return err
			}
			if err := configureMasters(cfg.networks, cfg.bridges); err != nil {
				return err
			}

			//nolint:nestif
			if !cfg.isContainer {
				if err := installPackages(ctx, cfg.yumMirrors, cfg.packages); err != nil {
					return err
				}
				if cfg.requireVirt {
					if err := pruneVirt(); err != nil {
						return err
					}
				}
				if err := configureLimits(); err != nil {
					return err
				}
				if err := configureHugePages(cfg.hugePages); err != nil {
					return err
				}
			}

			if cfg.requireIPForwarding {
				if err := configureIPForwarding(); err != nil {
					return err
				}
			}
			if err := runPrepares(ctx, cfg.prepare); err != nil {
				return err
			}
			return runServices(ctx, cfg.services)
		})
		return nil
	})

	switch {
	case errors.Is(err, ErrPowerOff):
		return errors.WithStack(syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF))
	case errors.Is(err, ErrReboot):
		return errors.WithStack(syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART))
	default:
		return err
	}
}

// ConfigureKernelModules loads kernel modules.
func ConfigureKernelModules(kernelModules []kernel.Module) error {
	for _, m := range kernelModules {
		if err := kernel.LoadModule(m); err != nil {
			return err
		}
	}
	return nil
}

func configureHostname(hostname string) error {
	return errors.WithStack(os.WriteFile("/etc/hostname", []byte(hostname+"\n"), 0o644))
}

func configureDNS(dns []net.IP) error {
	if err := os.MkdirAll("/etc", 0o755); err != nil {
		return errors.WithStack(err)
	}

	if err := os.WriteFile("/etc/hosts",
		[]byte(`127.0.0.1   localhost localhost.localdomain localhost4 localhost4.localdomain4
::1         localhost localhost.localdomain localhost6 localhost6.localdomain6
`), 0o644); err != nil {
		return errors.WithStack(err)
	}

	f, err := os.OpenFile("/etc/resolv.conf", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return errors.WithStack(err)
	}
	defer f.Close()

	for _, d := range dns {
		if _, err := fmt.Fprintf(f, "nameserver %s\n", d); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func configureNetworks(networks []InterfaceConfig) error {
	if err := configureLoopback(); err != nil {
		return err
	}

	links, err := netlink.LinkList()
	if err != nil {
		return errors.WithStack(err)
	}

	for _, config := range networks {
		var found bool
		for _, l := range links {
			if bytes.Equal(config.MAC, l.Attrs().HardwareAddr) {
				if err := configureNetwork(l, config.Name, config.IPs); err != nil {
					return err
				}
				found = true
				break
			}
		}
		if !found {
			return errors.Errorf("link %s not found", config.MAC)
		}
	}

	return nil
}

func configureBridges(bridges []InterfaceConfig) error {
	for _, config := range bridges {
		bridge := &netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{
				Name:         config.Name,
				HardwareAddr: config.MAC,
			},
		}

		if err := netlink.LinkAdd(bridge); err != nil {
			return errors.WithStack(err)
		}

		l, err := netlink.LinkByName(config.Name)
		if err != nil {
			return errors.WithStack(err)
		}

		if err := configureNetwork(l, config.Name, config.IPs); err != nil {
			return err
		}
	}

	return nil
}

func configureVLANs(vlans []VLANConfig) error {
	for _, config := range vlans {
		parent, err := netlink.LinkByName(config.ParentName)
		if err != nil {
			return errors.WithStack(err)
		}

		vlan := &netlink.Vlan{
			LinkAttrs: netlink.LinkAttrs{
				Name:        config.Name,
				ParentIndex: parent.Attrs().Index,
			},
			VlanId:       config.VLANID,
			VlanProtocol: netlink.VLAN_PROTOCOL_8021Q,
		}

		if err := netlink.LinkAdd(vlan); err != nil {
			return errors.WithStack(err)
		}

		l, err := netlink.LinkByName(config.Name)
		if err != nil {
			return errors.WithStack(err)
		}

		if err := configureNetwork(l, config.Name, config.IPs); err != nil {
			return err
		}
	}

	return nil
}

func configureMasters(networks, bridges []InterfaceConfig) error {
	for _, config := range append(append([]InterfaceConfig{}, networks...), bridges...) {
		if config.MasterName == "" {
			continue
		}
		masterLink, err := netlink.LinkByName(config.MasterName)
		if err != nil {
			return errors.WithStack(err)
		}

		slaveLink, err := netlink.LinkByName(config.Name)
		if err != nil {
			return errors.WithStack(err)
		}

		if err := netlink.LinkSetMaster(slaveLink, masterLink); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func configureLoopback() error {
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return errors.WithStack(err)
	}
	if err := kernel.SetSysctl("net/ipv6/conf/lo/disable_ipv6", "1"); err != nil {
		return err
	}
	return errors.WithStack(netlink.LinkSetUp(lo))
}

func configureNetwork(l netlink.Link, name string, ips []net.IPNet) error {
	if l.Attrs().Name != name {
		if err := netlink.LinkSetName(l, name); err != nil {
			return errors.WithStack(err)
		}
	}
	if err := configureIPv6OnInterface(name); err != nil {
		return err
	}

	var ip6Found bool
	for _, ip := range ips {
		if len(ip.IP) == net.IPv6len {
			ip6Found = true
		}

		if err := netlink.AddrAdd(l, &netlink.Addr{
			IPNet: &ip,
		}); err != nil {
			return errors.WithStack(err)
		}
	}
	if err := netlink.LinkSetUp(l); err != nil {
		return errors.WithStack(err)
	}

	if !ip6Found {
		if err := kernel.SetSysctl(filepath.Join("net/ipv6/conf", name, "disable_ipv6"), "1"); err != nil {
			return err
		}
	}

	return nil
}

func configureGateway(gateway net.IP) error {
	if gateway == nil {
		return nil
	}

	links, err := netlink.LinkList()
	if err != nil {
		return errors.WithStack(err)
	}
	for _, l := range links {
		ips, err := netlink.AddrList(l, netlink.FAMILY_V4)
		if err != nil {
			return errors.WithStack(err)
		}
		for _, ip := range ips {
			if ip.Contains(gateway) {
				return errors.WithStack(netlink.RouteAdd(&netlink.Route{
					Scope:     netlink.SCOPE_UNIVERSE,
					LinkIndex: l.Attrs().Index,
					Gw:        gateway,
				}))
			}
		}
	}

	return errors.Errorf("no link found for gateway %q", gateway)
}

func configureRoutes(routes []Route) error {
	for _, r := range routes {
		if err := netlink.RouteAdd(&netlink.Route{
			Scope: netlink.SCOPE_UNIVERSE,
			Dst:   &r.Destination,
			Gw:    r.Gateway,
		}); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func configureIPv6OnInterface(lName string) error {
	if err := kernel.SetSysctl(filepath.Join("net/ipv6/conf", lName, "autoconf"), "0"); err != nil {
		return err
	}
	if err := kernel.SetSysctl(filepath.Join("net/ipv6/conf", lName, "accept_ra"), "0"); err != nil {
		return err
	}
	return kernel.SetSysctl(filepath.Join("net/ipv6/conf", lName, "addr_gen_mode"), "1")
}

func configureIPv6() error {
	if err := kernel.SetSysctl("net/ipv6/conf/default/addr_gen_mode", "1"); err != nil {
		return err
	}
	if err := kernel.SetSysctl("net/ipv6/conf/default/autoconf", "0"); err != nil {
		return err
	}
	if err := kernel.SetSysctl("net/ipv6/conf/default/accept_ra", "0"); err != nil {
		return err
	}
	if err := kernel.SetSysctl("net/ipv6/conf/all/addr_gen_mode", "1"); err != nil {
		return err
	}
	if err := kernel.SetSysctl("net/ipv6/conf/all/autoconf", "0"); err != nil {
		return err
	}
	return kernel.SetSysctl("net/ipv6/conf/all/accept_ra", "0")
}

func runPrepares(ctx context.Context, prepare []PrepareFn) error {
	for _, p := range prepare {
		if err := p(ctx); err != nil {
			return err
		}
	}
	return nil
}

func runServices(ctx context.Context, services []ServiceConfig) error {
	if len(services) == 0 {
		return errors.New("no services defined")
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		appTerminatedCh := make(chan struct{})
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGCHLD)

		spawn("zombie", parallel.Fail, func(ctx context.Context) error {
			return zombie.Run(ctx, sigCh, appTerminatedCh)
		})
		spawn("services", parallel.Exit, func(ctx context.Context) error {
			defer close(appTerminatedCh)

			return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
				for _, s := range services {
					spawn(s.Name, s.OnExit, func(ctx context.Context) error {
						log := logger.Get(ctx)

						log.Info("Starting service")
						defer log.Info("Service stopped")

						return s.TaskFn(ctx)
					})
				}
				return nil
			})
		})

		return nil
	})
}

func configureEnv(hostname string) error {
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		return errors.WithStack(err)
	}

	for k, v := range map[string]string{
		"PATH":     "/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin",
		"HOME":     "/root",
		"USER":     "root",
		"TERM":     "xterm-256color",
		"HOSTNAME": hostname,
	} {
		if err := os.Setenv(k, v); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func installPackages(ctx context.Context, repoMirrors, packages []string) error {
	m := map[string]struct{}{}
	for _, p := range packages {
		m[p] = struct{}{}
	}

	if len(m) == 0 {
		return nil
	}

	pkgs := make([]string, 0, len(m))
	for p := range m {
		pkgs = append(pkgs, p)
	}

	sort.Strings(pkgs)

	if err := os.WriteFile("/etc/yum.repos.d/cloudless.mirrors",
		[]byte(strings.Join(repoMirrors, "\n")), 0o600); err != nil {
		return errors.WithStack(err)
	}
	if err := os.WriteFile("/etc/yum.repos.d/cloudless.repo", cloudlessRepo, 0o600); err != nil {
		return errors.WithStack(err)
	}

	// TODO (wojciech): One day I will write an rpm package manager in go.
	return libexec.Exec(ctx, exec.Command("dnf", append(
		[]string{
			"install", "-y",
			"--setopt=keepcache=False",
			"--setopt=install_weak_deps=False",
			"--repo=cloudless",
		}, pkgs...)...))
}

func configureIPForwarding() error {
	if err := kernel.SetSysctl("net/ipv4/conf/all/forwarding", "1"); err != nil {
		return err
	}
	return errors.WithStack(kernel.SetSysctl("net/ipv6/conf/all/forwarding", "1"))
}

func buildInitramfs() error {
	if err := os.MkdirAll("/boot", 0o555); err != nil {
		return errors.WithStack(err)
	}
	dF, err := os.OpenFile("/boot/initramfs", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.WithStack(err)
	}
	defer dF.Close()

	cW := gzip.NewWriter(dF)
	defer cW.Close()

	w := cpio.NewWriter(cW)
	defer w.Close()

	if err := addFile(w, 0o600, "/oldroot/distro.tar"); err != nil {
		return err
	}
	return addFile(w, 0o700, "/oldroot/init")
}

func addFile(w *cpio.Writer, mode cpio.FileMode, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return errors.WithStack(err)
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return errors.WithStack(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return errors.WithStack(err)
	}

	if err := w.WriteHeader(&cpio.Header{
		Name: filepath.Base(file),
		Size: size,
		Mode: mode,
	}); err != nil {
		return errors.WithStack(err)
	}

	_, err = io.Copy(w, f)
	return errors.WithStack(err)
}

func removeOldRoot() error {
	items, err := os.ReadDir("/oldroot")
	if err != nil {
		return errors.WithStack(err)
	}
	for _, item := range items {
		if err := os.RemoveAll(filepath.Join("/oldroot", item.Name())); err != nil {
			return errors.WithStack(err)
		}
	}
	if err := syscall.Unmount("/oldroot", 0); err != nil {
		return errors.WithStack(err)
	}
	return errors.WithStack(os.RemoveAll("/oldroot"))
}

func configureFirewall(sources []firewall.RuleSource) error {
	chains, err := firewall.EnsureChains()
	if err != nil {
		return err
	}

	conn := &nftables.Conn{}

	for _, s := range sources {
		rules, err := s(chains)
		if err != nil {
			return err
		}
		for _, r := range rules {
			r.Table = r.Chain.Table
			conn.AddRule(r)
		}
	}
	return errors.WithStack(conn.Flush())
}

func configureLimits() error {
	if err := os.MkdirAll("/etc/security", 0o755); err != nil {
		return errors.WithStack(err)
	}

	limitsF, err := os.OpenFile("/etc/security/limits.conf", os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return errors.WithStack(err)
	}
	defer limitsF.Close()

	_, err = limitsF.WriteString("\n* soft memlock unlimited\n* hard memlock unlimited\n")
	return errors.WithStack(err)
}

func configureHugePages(hugePages uint64) error {
	if hugePages == 0 {
		return nil
	}

	return errors.WithStack(os.WriteFile("/proc/sys/vm/nr_hugepages",
		[]byte(strconv.FormatUint(hugePages, 10)), 0o644))
}

func configureMounts(mounts []mountConfig) error {
	for _, m := range mounts {
		info, err := os.Stat(m.Source)
		if err != nil {
			if err := os.MkdirAll(m.Source, 0o700); err != nil {
				return errors.WithStack(err)
			}
			var err error
			info, err = os.Stat(m.Source)
			if err != nil {
				return errors.WithStack(err)
			}
		}

		var flags uintptr
		var fsType, fsOptions string

		// Is it a block device?
		var blockDev bool
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			blockDev = stat.Mode&syscall.S_IFBLK == syscall.S_IFBLK
			if blockDev {
				fsType = filesystem
				fsOptions = btrfsOptions
			} else {
				flags = syscall.MS_BIND | syscall.MS_PRIVATE
			}
		}

		//nolint:nestif
		if info.IsDir() || blockDev {
			if err := os.MkdirAll(m.Target, 0o700); err != nil {
				return errors.WithStack(err)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(m.Target), 0o700); err != nil {
				return errors.WithStack(err)
			}
			err := func() error {
				f, err := os.OpenFile(m.Target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, info.Mode())
				if err != nil {
					if os.IsExist(err) {
						return nil
					}
					return errors.WithStack(err)
				}
				return errors.WithStack(f.Close())
			}()
			if err != nil {
				return err
			}
		}
		if err := syscall.Mount(m.Source, m.Target, fsType, flags, fsOptions); err != nil {
			return errors.WithStack(err)
		}
		if !m.Writable {
			if err := syscall.Mount(m.Source, m.Target, fsType, flags|syscall.MS_REMOUNT|syscall.MS_RDONLY,
				""); err != nil {
				return errors.WithStack(err)
			}
		}
	}

	return nil
}

func setupVirt(c *Configuration) {
	c.RequirePackages(virtPackages...)
	c.StartServices(ServiceConfig{
		Name:   "virt",
		OnExit: parallel.Fail,
		TaskFn: func(ctx context.Context) error {
			configF, err := os.OpenFile("/etc/libvirt/qemu.conf", os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return errors.WithStack(err)
			}
			defer configF.Close()

			if _, err := configF.WriteString("\nuser = \"root\"\ngroup = \"root\"\n"); err != nil {
				return errors.WithStack(err)
			}

			ctxDaemonsCh := make(chan func(), 1)
			return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
				spawn("supervisor", parallel.Fail, func(ctx context.Context) error {
					select {
					case <-ctx.Done():
						return errors.WithStack(ctx.Err())
					case cancel := <-ctxDaemonsCh:
						defer cancel()
					}

					<-ctx.Done()

					if err := stopVMs(tcontext.Reopen(ctx)); err != nil {
						return err
					}

					return errors.WithStack(ctx.Err())
				})
				spawn("daemons", parallel.Fail, func(ctx context.Context) error {
					ctx, cancel := context.WithCancel(tcontext.Reopen(ctx))
					ctxDaemonsCh <- cancel
					return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
						// Available services: "virtqemud", "virtlogd", "virtstoraged", "virtnetworkd", "virtnodedevd"
						for _, c := range []string{"virtqemud", "virtlogd"} {
							spawn(c, parallel.Fail, func(ctx context.Context) error {
								return libexec.Exec(ctx, exec.Command(filepath.Join("/usr/sbin", c)))
							})
						}

						return nil
					})
				})

				return nil
			})
		},
	})
}

func pruneVirt() error {
	return errors.WithStack(filepath.WalkDir("/etc/libvirt/qemu", func(path string, d os.DirEntry, err error) error {
		if !d.IsDir() {
			return os.Remove(path)
		}
		return nil
	}))
}

func isContainer() bool {
	return os.Getenv(ContainerEnvVar) != ""
}

func unmount() error {
	mountsRaw, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return errors.Wrap(err, "reading /proc/mounts failed")
	}

	mounts := []string{}
	for mount := range strings.SplitSeq(string(mountsRaw), "\n") {
		props := strings.SplitN(mount, " ", 3)
		if len(props) < 2 {
			// last empty line
			break
		}
		mountPoint := props[1]

		// Managed by vmlinuz.
		if mountPoint != "/" {
			mounts = append(mounts, mountPoint)
		}
	}

	// Thanks to this trick shorter strings, come first.
	// If we unmount from the end of the slice, it is guaranteed that child mounts are unmounted before parents.
	sort.Strings(mounts)

	for i := len(mounts) - 1; i >= 0; i-- {
		if err := syscall.Unmount(mounts[i], 0); err != nil {
			return errors.Wrapf(err, "unmounting failed: %s", mounts[i])
		}
	}

	return nil
}

func stopVMs(ctx context.Context) error {
	lv := libvirt.NewWithDialer(dialers.NewLocal(dialers.WithSocket(qemuSocket)))
	defer func() {
		_ = lv.Disconnect()
	}()

	if err := lv.Connect(); err != nil {
		return errors.WithStack(err)
	}

	domains, _, err := lv.ConnectListAllDomains(1,
		libvirt.ConnectListDomainsActive|libvirt.ConnectListDomainsInactive)
	if err != nil {
		return errors.WithStack(err)
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for _, d := range domains {
			spawn("stopVM", parallel.Continue, func(ctx context.Context) error {
				log := logger.Get(ctx)

				for trial := 0; ; trial++ {
					active, err := lv.DomainIsActive(d)
					if err != nil {
						if libvirt.IsNotFound(err) {
							return nil
						}
						return errors.WithStack(err)
					}

					if active == 0 {
						return nil
					}

					err = lv.DomainShutdown(d)
					switch {
					case err == nil:
						if trial%10 == 0 {
							log.Info("VM is still running", zap.String("vm", d.Name))
						}
						<-time.After(time.Second)
					case libvirt.IsNotFound(err):
						return nil
					default:
						return errors.WithStack(err)
					}
				}
			})
		}

		return nil
	})
}
