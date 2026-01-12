package main

import (
	"net/http"
	"strconv"

	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/acpi"
	"github.com/outofforest/cloudless/pkg/container"
	containercache "github.com/outofforest/cloudless/pkg/container/cache"
	"github.com/outofforest/cloudless/pkg/dns"
	"github.com/outofforest/cloudless/pkg/eye"
	"github.com/outofforest/cloudless/pkg/grafana"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/ingress"
	"github.com/outofforest/cloudless/pkg/loki"
	"github.com/outofforest/cloudless/pkg/ntp"
	"github.com/outofforest/cloudless/pkg/prometheus"
	"github.com/outofforest/cloudless/pkg/shield"
	"github.com/outofforest/cloudless/pkg/ssh"
)

var monContainer = BoxFactory(
	dns.DNS(),
	eye.SendMetrics("http://10.255.0.3"),
	eye.RemoteLogging("http://10.255.0.4"),
	containercache.Mirrors("http://10.101.0.4:81"),
)

var HostMonitoring = Join(
	Box("monitoring",
		dns.DNS(),
		eye.SendMetrics("http://10.255.0.3"),
		eye.RemoteLogging("http://10.255.0.4"),
		eye.SystemMonitor(),
		acpi.PowerService(),
		ntp.Service(),
		shield.Open("tcp4", "igw", ssh.Port),
		ssh.Service("AAAAC3NzaC1lZDI1NTE5AAAAIEcJvvtOBgTsm3mq3Sg8cjn6Mz/vC9f3k6a89ZOjIyF6"),

		MountPersistentBase("vda"),
		Network("fc:ff:ff:fe:00:01", "igw", IPs("10.101.0.3/24")),
		Gateway("10.101.0.1"),
		shield.Expose("tcp", "10.101.0.3", 81, "10.255.0.3", prometheus.Port),
		shield.Expose("tcp", "10.101.0.3", 82, "10.255.0.4", loki.Port),
		shield.Expose("tcp", "10.101.0.3", ingress.PortHTTP, "10.255.0.5", ingress.PortHTTP),
		shield.Masquerade("brint", "igw"),
		Bridge("brint", "fc:ff:ff:fe:01:01", IPs("10.255.0.1/24")),
		container.New("grafana",
			container.Network("brint", "vgrafana", "fc:ff:ff:fe:01:02"),
		),
		container.New("prometheus",
			container.Network("brint", "vprometheus", "fc:ff:ff:fe:01:03"),
		),
		container.New("loki",
			container.Network("brint", "vloki", "fc:ff:ff:fe:01:04"),
		),
		container.New("ingress",
			container.Network("brint", "vingress", "fc:ff:ff:fe:01:05"),
		),
	),
	monContainer("grafana",
		Network("fc:ff:ff:fe:01:02", "igw", IPs("10.255.0.2/24")),
		Gateway("10.255.0.1"),
		shield.Open("tcp4", "igw", grafana.Port),
		grafana.Container("grafana",
			grafana.DataSource("Prometheus", grafana.DataSourcePrometheus, "http://10.255.0.3:"+strconv.Itoa(prometheus.Port)),
			grafana.DataSource("Loki", grafana.DataSourceLoki, "http://10.255.0.4:"+strconv.Itoa(loki.Port)),
			grafana.Dashboards(host.DashboardBoxes, eye.Dashboard),
		),
	),
	monContainer("prometheus",
		Network("fc:ff:ff:fe:01:03", "igw", IPs("10.255.0.3/24")),
		Gateway("10.255.0.1"),
		shield.Open("tcp4", "igw", prometheus.Port),
		prometheus.Container("prometheus"),
	),
	monContainer("loki",
		Network("fc:ff:ff:fe:01:04", "igw", IPs("10.255.0.4/24")),
		Gateway("10.255.0.1"),
		shield.Open("tcp4", "igw", loki.Port),
		loki.Container("loki"),
	),
	monContainer("ingress",
		Network("fc:ff:ff:fe:01:05", "igw", IPs("10.255.0.5/24")),
		Gateway("10.255.0.1"),
		shield.Open("tcp4", "igw", ingress.PortHTTP),
		ingress.Service(
			ingress.Endpoint("grafana",
				ingress.Domains("grafana.mon.local"),
				ingress.HTTPS(ingress.HTTPSModeDisabled),
				ingress.Methods(http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete),
				ingress.BodyLimit(4096),
				ingress.EnableWebsockets(),
				ingress.PlainBindings("10.255.0.5:80"),
			),
			ingress.Target("grafana", "10.255.0.2", grafana.Port, "/"),
		),
	),
)
