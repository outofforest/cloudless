package main

import (
	"net/http"
	"strconv"

	. "github.com/outofforest/cloudless" //nolint:stylecheck
	"github.com/outofforest/cloudless/pkg/acme"
	"github.com/outofforest/cloudless/pkg/acpi"
	"github.com/outofforest/cloudless/pkg/cnet"
	"github.com/outofforest/cloudless/pkg/container"
	containercache "github.com/outofforest/cloudless/pkg/container/cache"
	"github.com/outofforest/cloudless/pkg/dns"
	"github.com/outofforest/cloudless/pkg/eye"
	"github.com/outofforest/cloudless/pkg/grafana"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/host/firewall"
	"github.com/outofforest/cloudless/pkg/ingress"
	"github.com/outofforest/cloudless/pkg/loki"
	"github.com/outofforest/cloudless/pkg/ntp"
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/prometheus"
	"github.com/outofforest/cloudless/pkg/pxe"
	"github.com/outofforest/cloudless/pkg/ssh"
	"github.com/outofforest/cloudless/pkg/vm"
	"github.com/outofforest/cloudless/pkg/vnet"
	"github.com/outofforest/cloudless/pkg/yum"
)

const (
	endpointGrafana = "grafana"
)

var (
	// Host configures hosts.
	Host = BoxFactory(
		YumMirrors("http://10.0.0.100"),
		acpi.PowerService(),
		ntp.Service(),
		ssh.Service("AAAAC3NzaC1lZDI1NTE5AAAAIEcJvvtOBgTsm3mq3Sg8cjn6Mz/vC9f3k6a89ZOjIyF6"),
	)

	// Container configures containers.
	Container = BoxFactory(
		ContainerMirrors("http://10.0.0.100:81"),
	)

	// HostDNS configures DNS virtual machine.
	HostDNS = ExtendBoxFactory(Host,
		dns.Service(
			dns.ACME(),
			dns.Zone("dev.onem.network", "ns1.dev.onem.network", "wojtek@exw.co", 1,
				dns.Nameservers("ns1.dev.onem.network", "ns2.dev.onem.network"),
				dns.Domain("ns1.dev.onem.network", "93.179.253.130"),
				dns.Domain("ns2.dev.onem.network", "93.179.253.131"),
				dns.Domain("dev.onem.network", "93.179.253.132"),
			),
		),
	)
)

var deployment = Deployment(
	ImmediateKernelModules(DefaultKernelModules...),
	DNS(DefaultDNS...),
	eye.Service("http://10.0.0.155:3001"),
	RemoteLogging("http://10.0.0.155:3002"),
	Host("pxe",
		Gateway("10.0.0.1"),
		Network("00:01:0a:00:00:05", "10.0.0.100/24", "fe80::1/10"),
		Mount("/dev/sdb", "/root/mounts/repos", true),
		pxe.Service("/dev/sda"),
		yum.Service("/root/mounts/repos/fedora", 1),
		containercache.Service("/root/mounts/repos/containers", 1),
	),
	Host("server",
		Gateway("93.179.253.129"),
		Network("00:01:0a:00:00:9b", "10.0.0.155/24"),
		Network("52:54:00:47:a8:b6", "93.179.253.130/27", "93.179.253.131/27", "93.179.253.132/27"),
		Firewall(
			// DNS.
			firewall.RedirectV4UDPPort("93.179.253.130", dns.Port, "10.0.3.2", dns.Port),
			firewall.RedirectV4UDPPort("93.179.253.131", dns.Port, "10.0.3.3", dns.Port),

			// Prometheus.
			firewall.RedirectV4TCPPort("10.0.0.155", 3001, "10.0.1.2", 3001),

			// Loki.
			firewall.RedirectV4TCPPort("10.0.0.155", 3002, "10.0.1.2", 3002),
		),
		Mount("/dev/sda", "/root/mounts/acme", true),
		acme.Service("/root/mounts/acme", acme.LetsEncryptStaging,
			acme.DNSACMEs("10.0.3.2", "10.0.3.3"),
			acme.Domains("dev.onem.network", "test.dev.onem.network")),
		ingress.Service(
			ingress.Config{
				Endpoints: map[ingress.EndpointID]ingress.Endpoint{
					endpointGrafana: {
						Path:            "/",
						AllowedDomains:  []string{"dev.onem.network"},
						AllowedMethods:  []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
						AllowWebsockets: true,
						HTTPSMode:       ingress.HTTPSModeDisabled,
						PlainBindings: []string{
							"93.179.253.132:80",
						},
					},
				},
				Targets: map[ingress.EndpointID][]ingress.Target{
					endpointGrafana: {
						{
							Host: "10.0.1.2",
							Port: 3000,
							Path: "/",
						},
					},
				},
			},
		),
		vnet.NAT("dns", "52:54:00:6a:94:c0", vnet.IP4("10.0.3.1/24")),
		vm.New("dns01", 2, 2, vm.Network("dns", "52:54:00:6a:94:c1")),
		vm.New("dns02", 2, 2, vm.Network("dns", "52:54:00:6a:94:c2")),
		vnet.NAT("monitoring", "52:54:00:6d:94:c0", vnet.IP4("10.0.1.1/24")),
		vm.New("monitoring", 5, 4, vm.Network("monitoring", "00:01:0a:00:02:05")),
		cnet.NAT("acme", cnet.IP4("10.0.2.1/24")),
		container.New("pebble", "/tmp/containers/pebble",
			container.Network("acme", "52:54:00:6e:94:c3")),
	),
	HostDNS("dns01",
		Gateway("10.0.3.1"),
		Network("52:54:00:6a:94:c1", "10.0.3.2/24"),
	),
	HostDNS("dns02",
		Gateway("10.0.3.1"),
		Network("52:54:00:6a:94:c2", "10.0.3.3/24"),
	),
	Container("pebble",
		Gateway("10.0.2.1"),
		Network("52:54:00:6e:94:c3", "10.0.2.5/24"),
		pebble.Container("/tmp/app/pebble", "10.0.3.2:53"),
	),
	Host("monitoring",
		Gateway("10.0.1.1"),
		Network("00:01:0a:00:02:05", "10.0.1.2/24"),
		Firewall(
			// Grafana.
			firewall.RedirectV4TCPPort("10.0.1.2", 3000, "10.0.2.2", grafana.Port),

			// Prometheus.
			firewall.RedirectV4TCPPort("10.0.1.2", 3001, "10.0.2.3", prometheus.Port),

			// Loki.
			firewall.RedirectV4TCPPort("10.0.1.2", 3002, "10.0.2.4", loki.Port),
		),
		cnet.NAT("monitoring", cnet.IP4("10.0.2.1/24")),
		container.New("grafana", "/tmp/containers/grafana",
			container.Network("monitoring", "52:54:00:6e:94:c0")),
		container.New("prometheus", "/tmp/containers/prometheus",
			container.Network("monitoring", "52:54:00:6e:94:c1")),
		container.New("loki", "/tmp/containers/loki",
			container.Network("monitoring", "52:54:00:6e:94:c2")),
	),
	Container("grafana",
		Gateway("10.0.2.1"),
		Network("52:54:00:6e:94:c0", "10.0.2.2/24"),
		grafana.Container("/tmp/app/grafana",
			grafana.DataSource("Prometheus", grafana.DataSourcePrometheus, "http://10.0.2.3:"+strconv.Itoa(prometheus.Port)),
			grafana.DataSource("Loki", grafana.DataSourceLoki, "http://10.0.2.4:"+strconv.Itoa(loki.Port)),
			grafana.Dashboards(host.DashboardBoxes),
		),
	),
	Container("prometheus",
		Gateway("10.0.2.1"),
		Network("52:54:00:6e:94:c1", "10.0.2.3/24"),
		prometheus.Container("/tmp/app/prometheus"),
	),
	Container("loki",
		Gateway("10.0.2.1"),
		Network("52:54:00:6e:94:c2", "10.0.2.4/24"),
		loki.Container("/tmp/app/loki"),
	),
)

func main() {
	Main(deployment...)
}
