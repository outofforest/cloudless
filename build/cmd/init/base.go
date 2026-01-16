package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/acpi"
	containercache "github.com/outofforest/cloudless/pkg/container/cache"
	"github.com/outofforest/cloudless/pkg/dev"
	"github.com/outofforest/cloudless/pkg/dns"
	"github.com/outofforest/cloudless/pkg/eye"
	"github.com/outofforest/cloudless/pkg/ntp"
	"github.com/outofforest/cloudless/pkg/shield"
	"github.com/outofforest/cloudless/pkg/ssh"
)

var (
	// Host configures hosts.
	Host = BoxFactory(
		dns.DNS(),
		eye.RemoteLogging(dev.LokiAddr),
		eye.SystemMonitor(),
		acpi.PowerService(),
		ntp.Service(),
		shield.Open("tcp4", "igw", ssh.Port),
		ssh.Service("AAAAC3NzaC1lZDI1NTE5AAAAIEcJvvtOBgTsm3mq3Sg8cjn6Mz/vC9f3k6a89ZOjIyF6"),
	)

	// Container configures container.
	Container = BoxFactory(
		dns.DNS(),
		shield.Open("tcp4", "igw", eye.MetricPort),
		eye.MetricsServer(),
		eye.RemoteLogging(dev.LokiAddr),
		containercache.Mirrors(dev.ContainerCacheAddr),
	)
)
