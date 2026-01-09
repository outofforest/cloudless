package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/dns"
	"github.com/outofforest/cloudless/pkg/shield"
)

var HostService = Join(
	Host("service",
		Network("02:00:00:00:00:02", "eth0", Master("igw")),
		Gateway("10.101.0.1"),
		Bridge("igw", "02:00:00:00:02:01", IPs("10.101.0.2/24")),
		container.New("dns", "/root/persistent/containers/cache",
			container.Network("igw", "vdns", "02:00:00:00:02:02"),
		),
	),
	Container("dns",
		Network("02:00:00:00:02:02", "igw", IPs("10.101.0.8/24")),
		Gateway("10.101.0.1"),
		shield.Open("udp4", "igw", dns.Port),
		dns.Service(
			dns.Zone("example.local", "ns1.example.local", "wojtek@exw.co", 1,
				dns.Nameservers("ns1.example.local", "ns2.example.local"),
				dns.Domain("ns1.example.local", "10.101.0.155"),
				dns.Domain("ns2.example.local", "10.101.0.156"),
				dns.Domain("test.example.local", "10.101.0.155"),
			),
		),
	),
)
