package main

import (
	"net/http"
	"strconv"

	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/acme"
	"github.com/outofforest/cloudless/pkg/acpi"
	"github.com/outofforest/cloudless/pkg/container"
	containercache "github.com/outofforest/cloudless/pkg/container/cache"
	"github.com/outofforest/cloudless/pkg/dns"
	dnsdkim "github.com/outofforest/cloudless/pkg/dns/dkim"
	"github.com/outofforest/cloudless/pkg/email"
	"github.com/outofforest/cloudless/pkg/eye"
	"github.com/outofforest/cloudless/pkg/grafana"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/ingress"
	"github.com/outofforest/cloudless/pkg/loki"
	"github.com/outofforest/cloudless/pkg/ntp"
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/profile"
	"github.com/outofforest/cloudless/pkg/prometheus"
	"github.com/outofforest/cloudless/pkg/pxe"
	pxedhcp6 "github.com/outofforest/cloudless/pkg/pxe/dhcp6"
	pxetftp "github.com/outofforest/cloudless/pkg/pxe/tftp"
	"github.com/outofforest/cloudless/pkg/shield"
	"github.com/outofforest/cloudless/pkg/ssh"
	"github.com/outofforest/cloudless/pkg/vlan"
	"github.com/outofforest/cloudless/pkg/vm"
	"github.com/outofforest/cloudless/pkg/yum"
)

const (
	endpointGrafana ingress.EndpointID = "grafana"
)

var (
	// Host configures hosts.
	Host = BoxFactory(
		eye.SendMetrics("http://10.0.4.3:80"),
		eye.RemoteLogging("http://10.0.4.4:80"),
		yum.Mirrors("http://10.0.0.100"),
		acpi.PowerService(),
		ntp.Service(),
		eye.SystemMonitor(),
		shield.Open("tcp4", "igw", ssh.Port),
		ssh.Service("AAAAC3NzaC1lZDI1NTE5AAAAIEcJvvtOBgTsm3mq3Sg8cjn6Mz/vC9f3k6a89ZOjIyF6"),
	)

	// Container configures containers.
	Container = BoxFactory(
		eye.SendMetrics("http://10.0.4.3:80"),
		eye.RemoteLogging("http://10.0.4.4:80"),
		containercache.Mirrors("http://10.0.0.100:81"),
	)

	// HostDNS configures DNS virtual machine.
	HostDNS = ExtendBoxFactory(Host,
		shield.Open("udp4", "igw", dns.Port),
		shield.Open("tcp4", "igw", dnsdkim.Port),
		dns.Service(
			dns.ACME(),
			dns.DKIM(),
			dns.Zone("dev.onem.network", "ns1.dev.onem.network", "wojtek@exw.co", 1,
				dns.Nameservers("ns1.dev.onem.network", "ns2.dev.onem.network"),
				dns.Domain("ns1.dev.onem.network", "93.179.253.130"),
				dns.Domain("ns2.dev.onem.network", "93.179.253.131"),
				dns.Domain("dev.onem.network", "93.179.253.132"),
				dns.Domain("mail.dev.onem.network", "93.179.253.133"),
				dns.Text("dev.onem.network", "protonmail-verification=778c4a1d7c009f47fd4c29b53d3ec2e7e0c00ce4"),
				dns.MailExchange("mail.protonmail.ch", 10),
				dns.MailExchange("mailsec.protonmail.ch", 20),
				dns.Text("dev.onem.network", "v=spf1 a:mail.dev.onem.network include:_spf.protonmail.ch ~all"),
				dns.Alias("protonmail._domainkey.dev.onem.network", "protonmail.domainkey.dgd2ylhxf2ktsqiwayacsln52gnwt3zfy6jhrbdijudy3c64my3pa.domains.proton.ch"),   //nolint:lll
				dns.Alias("protonmail2._domainkey.dev.onem.network", "protonmail2.domainkey.dgd2ylhxf2ktsqiwayacsln52gnwt3zfy6jhrbdijudy3c64my3pa.domains.proton.ch"), //nolint:lll
				dns.Alias("protonmail3._domainkey.dev.onem.network", "protonmail3.domainkey.dgd2ylhxf2ktsqiwayacsln52gnwt3zfy6jhrbdijudy3c64my3pa.domains.proton.ch"), //nolint:lll
				dns.Text("_dmarc.dev.onem.network", "v=DMARC1;p=quarantine"),
			),
		),
	)
)

var deployment = Deployment(
	ImmediateKernelModules(DefaultKernelModules...),
	DNS(DefaultDNS...),
	Host("pxe",
		Gateway("10.0.0.1"),
		Network("02:00:00:00:01:01", "igw", IPs("10.0.0.100/24", "fe80::1/10")),
		Route("10.0.4.0/24", "10.0.0.155"),
		Mount("/dev/sdb", "/root/mounts/repos", true),
		shield.Open("udp6", "igw", pxedhcp6.Port),
		shield.Open("udp6", "igw", pxetftp.Port),
		pxe.Service("/dev/sda"),
		shield.Open("tcp4", "igw", yum.Port),
		yum.Service("/root/mounts/repos/fedora", 1),
		shield.Open("tcp4", "igw", containercache.Port),
		containercache.Service("/root/mounts/repos/containers", 1),
	),
	Host("server",
		Gateway("93.179.253.129"),
		Bridge("igw", "02:00:00:00:08:01",
			IPs("93.179.253.130/27", "93.179.253.131/27", "93.179.253.133/27"),
		),
		Network("02:00:00:00:02:01", "iint", IPs("10.0.0.155/24")),
		Network("02:00:00:00:02:02", "ipub", Master("igw")),
		vlan.New("vlan100", "igw", vlan.IPs("10.100.0.155/24")),
		Route("10.0.4.0/24", "10.0.1.2"),
		shield.Forward("iint", "brmon"),

		// Profiler.
		shield.Open("tcp4", "iint", profile.Port),
		profile.Service(),

		// DNS.
		Bridge("brdns", "02:00:00:00:03:01", IPs("10.0.3.1/24")),
		shield.Masquerade("brdns", "igw"),
		shield.Forward("brdns", "brmon"),
		shield.Expose("udp", "93.179.253.130", dns.Port, "10.0.3.2", dns.Port),
		shield.Expose("udp", "93.179.253.131", dns.Port, "10.0.3.3", dns.Port),
		vm.New("dns01", 2, 2, vm.Bridge("brdns", "vdns01", "02:00:00:00:03:02")),
		vm.New("dns02", 2, 2, vm.Bridge("brdns", "vdns02", "02:00:00:00:03:03")),

		// Ingress.
		Bridge("bringress", "02:00:00:00:09:01", IPs("10.0.6.1/24")),
		shield.Forward("bringress", "brmon"),
		shield.Forward("bringress", "bracme"),
		container.New("ingress", "/tmp/containers/ingress",
			container.Network("igw", "vingresspub", "02:00:00:00:08:02"),
			container.Network("bringress", "vingressint", "02:00:00:00:09:02"),
		),

		// Monitoring.
		Bridge("brmon", "02:00:00:00:04:01", IPs("10.0.1.1/24")),
		shield.Masquerade("brmon", "igw"),
		shield.Masquerade("brmon", "iint"),
		vm.New("monitoring", 5, 4, vm.Bridge("brmon", "vmon", "02:00:00:00:04:02")),

		// ACME.
		Bridge("bracme", "02:00:00:00:05:01", IPs("10.0.2.1/24")),
		shield.Masquerade("bracme", "igw"),
		shield.Masquerade("bracme", "iint"),
		shield.Forward("bracme", "brdns"),
		shield.Forward("bracme", "brmon"),
		Mount("/dev/sda", "/root/mounts/acme", true),
		container.New("pebble", "/tmp/containers/pebble",
			container.Network("bracme", "vpebble", "02:00:00:00:05:02"),
		),
		container.New("acme", "/tmp/containers/acme",
			container.Network("bracme", "vacme", "02:00:00:00:05:03"),
		),

		// Mailer.
		Bridge("brmail", "02:00:00:00:07:01", IPs("10.0.5.1/24")),
		shield.Source("brmail", "igw", "93.179.253.133"),
		shield.Forward("brmail", "brdns"),
		shield.Forward("brmail", "brmon"),
		container.New("mailer", "/tmp/containers/mailer",
			container.Network("brmail", "vmailer", "02:00:00:00:07:02"),
		),
	),
	HostDNS("dns01",
		Gateway("10.0.3.1"),
		Network("02:00:00:00:03:02", "igw", IPs("10.0.3.2/24")),
	),
	HostDNS("dns02",
		Gateway("10.0.3.1"),
		Network("02:00:00:00:03:03", "igw", IPs("10.0.3.3/24")),
	),
	Container("ingress",
		Gateway("93.179.253.129"),
		Network("02:00:00:00:08:02", "igw", IPs("93.179.253.132/27")),
		Network("02:00:00:00:09:02", "iint", IPs("10.0.6.2/24")),
		Route("10.0.2.0/24", "10.0.6.1"),
		Route("10.0.4.0/24", "10.0.6.1"),
		shield.Open("tcp4", "igw", ingress.PortHTTP),
		shield.Open("tcp4", "igw", ingress.PortHTTPS),
		ingress.Service(
			ingress.CertificateURL("http://10.0.2.6:"+strconv.FormatUint(acme.Port, 10)),
			ingress.Endpoint(endpointGrafana,
				ingress.Domains("dev.onem.network"),
				ingress.Methods(http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete),
				ingress.BodyLimit(4096),
				ingress.EnableWebsockets(),
				ingress.TLSBindings("93.179.253.132:443"),
			),
			ingress.Target(endpointGrafana, "10.0.4.2", grafana.Port, "/"),
		),
	),
	Container("pebble",
		Gateway("10.0.2.1"),
		Network("02:00:00:00:05:02", "igw", IPs("10.0.2.5/24")),
		shield.Open("tcp4", "igw", pebble.Port),
		pebble.Container("/tmp/app/pebble", "10.0.3.2:53"),
	),
	Container("acme",
		Gateway("10.0.2.1"),
		Network("02:00:00:00:05:03", "igw", IPs("10.0.2.6/24")),
		Mount("/root/mounts/acme", "/acme", true),
		shield.Open("tcp4", "igw", acme.Port),
		acme.Service("/acme", "wojtek@exw.co", acme.LetsEncryptStaging,
			acme.Waves("10.0.3.2", "10.0.3.3"),
			acme.Domains("dev.onem.network"),
		),
	),
	Container("mailer",
		Gateway("10.0.5.1"),
		Network("02:00:00:00:07:02", "igw", IPs("10.0.5.2/24")),
		email.Service(
			email.DNSDKIMs("10.0.3.2", "10.0.3.3"),
		),
	),
	Host("monitoring",
		Gateway("10.0.1.1"),
		Network("02:00:00:00:04:02", "igw", IPs("10.0.1.2/24")),
		Bridge("brmon", "02:00:00:00:06:01", IPs("10.0.4.1/24")),
		shield.Masquerade("brmon", "igw"),
		shield.Forward("igw", "brmon"),
		container.New("grafana", "/tmp/containers/grafana",
			container.Network("brmon", "vgrafana", "02:00:00:00:06:02"),
		),
		container.New("prometheus", "/tmp/containers/prometheus",
			container.Network("brmon", "vprometheus", "02:00:00:00:06:03"),
		),
		container.New("loki", "/tmp/containers/loki",
			container.Network("brmon", "vloki", "02:00:00:00:06:04"),
		),
	),
	Container("grafana",
		Gateway("10.0.4.1"),
		Network("02:00:00:00:06:02", "igw", IPs("10.0.4.2/24")),
		shield.Open("tcp4", "igw", grafana.Port),
		grafana.Container("/tmp/app/grafana",
			grafana.DataSource("Prometheus", grafana.DataSourcePrometheus, "http://10.0.4.3:"+strconv.Itoa(prometheus.Port)),
			grafana.DataSource("Loki", grafana.DataSourceLoki, "http://10.0.4.4:"+strconv.Itoa(loki.Port)),
			grafana.Dashboards(host.DashboardBoxes, eye.Dashboard),
		),
	),
	Container("prometheus",
		Gateway("10.0.4.1"),
		Network("02:00:00:00:06:03", "igw", IPs("10.0.4.3/24")),
		shield.Open("tcp4", "igw", prometheus.Port),
		prometheus.Container("/tmp/app/prometheus"),
	),
	Container("loki",
		Gateway("10.0.4.1"),
		Network("02:00:00:00:06:04", "igw", IPs("10.0.4.4/24")),
		shield.Open("tcp4", "igw", loki.Port),
		loki.Container("/tmp/app/loki"),
	),
)

func main() {
	Main(deployment...)
}
