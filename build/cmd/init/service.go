package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/acme"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/dns"
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/shield"
	"github.com/outofforest/cloudless/pkg/wave"
)

var HostService = Join(
	Host("service",
		Network("02:00:00:00:00:02", "eth0", Master("igw")),
		Gateway("10.101.0.1"),
		Bridge("igw", "02:00:00:00:02:01", IPs("10.101.0.2/24")),
		container.New("wave",
			container.Network("igw", "vwave", "02:00:00:00:02:02"),
		),
		container.New("dns",
			container.Network("igw", "vdns", "02:00:00:00:02:03"),
		),
		container.New("pebble",
			container.Network("igw", "vpebble", "02:00:00:00:02:04"),
		),
		container.New("acme",
			container.Network("igw", "vacme", "02:00:00:00:02:05"),
		),
	),
	Container("wave",
		Network("02:00:00:00:02:02", "igw", IPs("10.101.0.8/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", wave.Port),
		wave.Service(20*1024),
	),
	Container("dns",
		Network("02:00:00:00:02:03", "igw", IPs("10.101.0.9/24")),
		Gateway("10.101.0.1"),
		shield.Open("udp4", "igw", dns.Port),
		dns.Service(
			dns.Waves("10.101.0.8"),
			dns.ACME(),
			dns.Zone("example.local", "ns1.example.local", "wojtek@exw.co", 1,
				dns.Nameservers("ns1.example.local", "ns2.example.local"),
				dns.Domain("ns1.example.local", "10.101.0.155"),
				dns.Domain("ns2.example.local", "10.101.0.156"),
				dns.Domain("test.example.local", "10.101.0.155"),
			),
		),
	),
	Container("pebble",
		Network("02:00:00:00:02:04", "igw", IPs("10.101.0.10/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", pebble.Port),
		pebble.Container("pebble", "10.101.0.9:53"),
	),
	Container("acme",
		Network("02:00:00:00:02:05", "igw", IPs("10.101.0.11/24")),
		Gateway("10.101.0.1"),
		container.AppMount("acme"),
		shield.Open("tcp4", "igw", acme.Port),
		acme.Service("acme", "wojtek@exw.co", acme.Pebble("10.101.0.10"),
			acme.Waves("10.101.0.8"),
			acme.Domains("test.example.local"),
		),
	),
)
