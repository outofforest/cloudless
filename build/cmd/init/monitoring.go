package main

import (
	"strconv"

	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/container"
	containercache "github.com/outofforest/cloudless/pkg/container/cache"
	"github.com/outofforest/cloudless/pkg/eye"
	"github.com/outofforest/cloudless/pkg/grafana"
	"github.com/outofforest/cloudless/pkg/host"
	"github.com/outofforest/cloudless/pkg/loki"
	"github.com/outofforest/cloudless/pkg/prometheus"
	"github.com/outofforest/cloudless/pkg/shield"
)

var HostMonitoring = Join(
	Host("monitoring",
		Mount("/dev/vda", "/root/persistent", true),
		Network("02:00:00:00:00:03", "eth0", Master("igw")),
		Gateway("10.101.0.1"),
		Bridge("igw", "02:00:00:00:01:01", IPs("10.101.0.3/24")),
		container.New("cache", "/root/persistent/containers/cache",
			container.Network("igw", "vcache", "02:00:00:00:01:02"),
		),
		container.New("grafana", "/root/persistent/containers/grafana",
			container.Network("igw", "vgrafana", "02:00:00:00:01:03"),
		),
		container.New("prometheus", "/root/persistent/containers/prometheus",
			container.Network("igw", "vprometheus", "02:00:00:00:01:04"),
		),
		container.New("loki", "/root/persistent/containers/loki",
			container.Network("igw", "vloki", "02:00:00:00:01:05"),
		),
	),
	Container("cache",
		Network("02:00:00:00:01:02", "igw", IPs("10.101.0.4/24")),
		Gateway("10.101.0.1"),
		Mount("/root/persistent/cache/containers", "/containers", true),
		shield.Open("tcp4", "igw", containercache.Port),
		containercache.Service("/containers", 1),
	),
	Container("grafana",
		Network("02:00:00:00:01:03", "igw", IPs("10.101.0.5/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", grafana.Port),
		grafana.Container("/root/persistent/apps/grafana",
			grafana.DataSource("Prometheus", grafana.DataSourcePrometheus, "http://10.101.0.6:"+strconv.Itoa(prometheus.Port)),
			grafana.DataSource("Loki", grafana.DataSourceLoki, "http://10.101.0.7:"+strconv.Itoa(loki.Port)),
			grafana.Dashboards(host.DashboardBoxes, eye.Dashboard),
		),
	),
	Container("prometheus",
		Network("02:00:00:00:01:04", "igw", IPs("10.101.0.6/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", prometheus.Port),
		prometheus.Container("/root/persistent/apps/prometheus"),
	),
	Container("loki",
		Network("02:00:00:00:01:05", "igw", IPs("10.101.0.7/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", loki.Port),
		loki.Container("/root/persistent/apps/loki"),
	),
)
