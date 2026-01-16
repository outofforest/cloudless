package dev

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
)

const (
	monitoringIP = "10.255.0.253"

	// LokiAddr is the address of loki service.
	LokiAddr = "http://" + monitoringIP + ":82"
)

var monContainer = BoxFactory(
	dns.DNS(),
	shield.Open("tcp4", "igw", eye.MetricPort),
	eye.MetricsServer(),
	eye.RemoteLogging("http://10.255.255.4"),
	containercache.Mirrors("http://10.255.0.254:81"),
)

func monitoringBox(metricServers []string) host.Configurator {
	return Join(
		Box("mon",
			dns.DNS(),
			shield.Open("tcp4", "brint", eye.MetricPort),
			eye.MetricsServer(eye.Addresses("10.255.255.2", "10.255.255.3", "10.255.255.4", "10.255.255.5", "10.255.255.6")),
			eye.RemoteLogging("http://10.255.255.4"),
			eye.SystemMonitor(),
			acpi.PowerService(),
			ntp.Service(),

			MountPersistentBase("vda"),
			Network("fc:ff:ff:fe:00:01", "igw", IPs("10.255.0.253/24")),
			Gateway("10.255.0.1"),
			shield.Expose("tcp", "10.255.0.253", 82, "10.255.255.4", loki.Port),
			shield.Expose("tcp", "10.255.0.253", ingress.PortHTTP, "10.255.255.5", ingress.PortHTTP),
			shield.Expose("udp", "10.255.0.253", dns.Port, "10.255.255.6", dns.Port),
			shield.Masquerade("brint", "igw"),
			Bridge("brint", "fc:ff:ff:fe:01:01", IPs("10.255.255.1/24")),
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
			container.New("dns",
				container.Network("brint", "vdns", "fc:ff:ff:fe:01:06"),
			),
		),
		monContainer("mon-grafana",
			Network("fc:ff:ff:fe:01:02", "igw", IPs("10.255.255.2/24")),
			Gateway("10.255.255.1"),
			shield.Open("tcp4", "igw", grafana.Port),
			grafana.Container("grafana",
				grafana.DataSource("Prometheus", grafana.DataSourcePrometheus,
					"http://10.255.255.3:"+strconv.Itoa(prometheus.Port)),
				grafana.DataSource("Loki", grafana.DataSourceLoki, "http://10.255.255.4:"+strconv.Itoa(loki.Port)),
				grafana.Dashboards(host.DashboardBoxes, eye.Dashboard),
			),
		),
		monContainer("mon-prometheus",
			Network("fc:ff:ff:fe:01:03", "igw", IPs("10.255.255.3/24")),
			Gateway("10.255.255.1"),
			shield.Open("tcp4", "igw", prometheus.Port),
			prometheus.Container("prometheus",
				prometheus.Targets(append([]string{"10.255.255.1:9000", "10.255.0.254:9000"}, metricServers...)...),
			),
		),
		monContainer("mon-loki",
			Network("fc:ff:ff:fe:01:04", "igw", IPs("10.255.255.4/24")),
			Gateway("10.255.255.1"),
			shield.Open("tcp4", "igw", loki.Port),
			loki.Container("loki"),
		),
		monContainer("mon-ingress",
			Network("fc:ff:ff:fe:01:05", "igw", IPs("10.255.255.5/24")),
			Gateway("10.255.255.1"),
			shield.Open("tcp4", "igw", ingress.PortHTTP),
			ingress.Service(
				ingress.Endpoint("grafana",
					ingress.Domains("grafana.mon.test"),
					ingress.HTTPS(ingress.HTTPSModeDisabled),
					ingress.Methods(http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete),
					ingress.BodyLimit(4096),
					ingress.EnableWebsockets(),
					ingress.PlainBindings("10.255.255.5:80"),
				),
				ingress.Target("grafana", "10.255.255.2", grafana.Port, "/"),
			),
		),
		monContainer("mon-dns",
			Network("fc:ff:ff:fe:01:06", "igw", IPs("10.255.255.6/24")),
			Gateway("10.255.255.1"),
			shield.Open("udp4", "igw", dns.Port),
			dns.Service(
				dns.Zone("mon.test", "ns1.mon.test", "wojtek@app.test", 1,
					dns.Nameservers("ns1.mon.test"),
					dns.Domain("ns1.mon.test", "10.255.0.253"),
					dns.Domain("grafana.mon.test", "10.255.0.253"),
				),
			),
		),
	)
}
