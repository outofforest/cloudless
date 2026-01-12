package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/acpi"
	containercache "github.com/outofforest/cloudless/pkg/container/cache"
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
		eye.SendMetrics("http://10.101.0.3:81"),
		eye.RemoteLogging("http://10.101.0.3:82"),
		eye.SystemMonitor(),
		acpi.PowerService(),
		ntp.Service(),
		shield.Open("tcp4", "igw", ssh.Port),
		ssh.Service("AAAAC3NzaC1lZDI1NTE5AAAAIEcJvvtOBgTsm3mq3Sg8cjn6Mz/vC9f3k6a89ZOjIyF6"),
	)

	// Container configures container.
	Container = BoxFactory(
		dns.DNS(),
		eye.SendMetrics("http://10.101.0.3:81"),
		eye.RemoteLogging("http://10.101.0.3:82"),
		containercache.Mirrors("http://10.101.0.4:81"),
	)
)
