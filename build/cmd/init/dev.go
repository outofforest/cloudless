package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/shield"
)

var HostDev = Join(
	Host("dev",
		Network("02:00:00:00:00:04", "eth0", Master("igw")),
		Gateway("10.101.0.1"),
		Bridge("igw", "02:00:00:00:03:01", IPs("10.101.0.11/24")),
		container.New("pebble",
			container.Network("igw", "vpebble", "02:00:00:00:03:02"),
		),
	),
	Container("pebble",
		Network("02:00:00:00:03:02", "igw", IPs("10.101.0.12/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", pebble.Port),
		pebble.Container("pebble", "10.101.0.4:53"),
	),
)
