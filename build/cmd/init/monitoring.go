package main

import (
	"net/http"
	"strconv"

	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/acme"
	"github.com/outofforest/cloudless/pkg/container"
	containercache "github.com/outofforest/cloudless/pkg/container/cache"
	"github.com/outofforest/cloudless/pkg/eye"
	"github.com/outofforest/cloudless/pkg/grafana"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/ingress"
	"github.com/outofforest/cloudless/pkg/loki"
	"github.com/outofforest/cloudless/pkg/prometheus"
	"github.com/outofforest/cloudless/pkg/shield"
)

var HostMonitoring = Join(
	Host("monitoring",
		MountPersistentBase("vda"),
		Network("02:00:00:00:00:03", "eth0", Master("igw")),
		Gateway("10.101.0.1"),
		Bridge("igw", "02:00:00:00:02:01", IPs("10.101.0.7/24")),
		container.New("cache",
			container.Network("igw", "vcache", "02:00:00:00:02:02"),
		),
		container.New("grafana",
			container.Network("igw", "vgrafana", "02:00:00:00:02:03"),
		),
		container.New("prometheus",
			container.Network("igw", "vprometheus", "02:00:00:00:02:04"),
		),
		container.New("loki",
			container.Network("igw", "vloki", "02:00:00:00:02:05"),
		),
		container.New("acme",
			container.Network("igw", "vacme", "02:00:00:00:02:06"),
		),
		container.New("ingress",
			container.Network("igw", "vingress", "02:00:00:00:02:07"),
		),
	),
	Container("cache",
		Network("02:00:00:00:02:02", "igw", IPs("10.101.0.8/24")),
		Gateway("10.101.0.1"),
		container.AppMount("container-cache"),
		shield.Open("tcp4", "igw", containercache.Port),
		containercache.Service("container-cache", 1),
	),
	Container("grafana",
		Network("02:00:00:00:02:03", "igw", IPs("10.101.0.9/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", grafana.Port),
		grafana.Container("grafana",
			grafana.DataSource("Prometheus", grafana.DataSourcePrometheus, "http://10.101.0.10:"+strconv.Itoa(prometheus.Port)),
			grafana.DataSource("Loki", grafana.DataSourceLoki, "http://10.101.0.11:"+strconv.Itoa(loki.Port)),
			grafana.Dashboards(host.DashboardBoxes, eye.Dashboard),
		),
	),
	Container("prometheus",
		Network("02:00:00:00:02:04", "igw", IPs("10.101.0.10/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", prometheus.Port),
		prometheus.Container("prometheus"),
	),
	Container("loki",
		Network("02:00:00:00:02:05", "igw", IPs("10.101.0.11/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", loki.Port),
		loki.Container("loki"),
	),
	Container("acme",
		Network("02:00:00:00:02:06", "igw", IPs("10.101.0.12/24")),
		Gateway("10.101.0.1"),
		container.AppMount("acme"),
		acme.Service("acme", "wojtek@exw.co", acme.Pebble("10.101.0.15"),
			acme.Waves("10.101.0.3"),
			acme.Domains("mon.local", "*.mon.local"),
		),
	),
	Container("ingress",
		Network("02:00:00:00:02:07", "igw", IPs("10.101.0.13/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", ingress.PortHTTP),
		shield.Open("tcp4", "igw", ingress.PortHTTPS),
		ingress.Service(
			ingress.Waves("10.101.0.3"),
			ingress.Endpoint("grafana",
				ingress.Domains("grafana.mon.local"),
				ingress.HTTPS(ingress.HTTPSModeOptional),
				ingress.Methods(http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete),
				ingress.BodyLimit(4096),
				ingress.EnableWebsockets(),
				ingress.PlainBindings("10.101.0.13:80"),
				ingress.TLSBindings("10.101.0.13:443"),
			),
			ingress.Target("grafana", "10.101.0.9", grafana.Port, "/"),
		),
	),
)
