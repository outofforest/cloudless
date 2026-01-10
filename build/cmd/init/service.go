package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/acme"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/dns"
	"github.com/outofforest/cloudless/pkg/shield"
	"github.com/outofforest/cloudless/pkg/wave"
)

var HostService = Join(
	Host("service",
		Network("02:00:00:00:00:02", "eth0", Master("igw")),
		Gateway("10.101.0.1"),
		Bridge("igw", "02:00:00:00:01:01", IPs("10.101.0.2/24")),
		container.New("wave",
			container.Network("igw", "vwave", "02:00:00:00:01:02"),
		),
		container.New("dns",
			container.Network("igw", "vdns", "02:00:00:00:01:03"),
		),
		container.New("acme",
			container.Network("igw", "vacme", "02:00:00:00:01:04"),
		),
	),
	Container("wave",
		Network("02:00:00:00:01:02", "igw", IPs("10.101.0.3/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", wave.Port),
		wave.Service(20*1024),
	),
	Container("dns",
		Network("02:00:00:00:01:03", "igw", IPs("10.101.0.4/24")),
		Gateway("10.101.0.1"),
		shield.Open("udp4", "igw", dns.Port),
		dns.Service(
			dns.Waves("10.101.0.3"),
			dns.ACME(),
			dns.DKIM(),
			dns.ForwardFor("10.101.0.0/24"),
			dns.ForwardTo(),
			dns.Zone("example.local", "ns1.example.local", "wojtek@exw.co", 1,
				dns.Nameservers("ns1.example.local", "ns2.example.local"),
				dns.MailExchange("smtp.example.local", 10),
				dns.Domain("ns1.example.local", "10.101.0.4"),
				dns.Domain("smtp.example.local", "10.101.0.13"),
				dns.Domain("mailer.example.local", "10.101.0.14"),
				dns.Domain("example.local", "10.101.0.8"),
				dns.Text("_dmarc.example.local", "v=DMARC1;p=quarantine"),
				dns.Text("example.local", "v=spf1 a:mailer.example.local ~all"),
			),
		),
	),
	Container("acme",
		Network("02:00:00:00:01:04", "igw", IPs("10.101.0.5/24")),
		Gateway("10.101.0.1"),
		container.AppMount("acme"),
		shield.Open("tcp4", "igw", acme.Port),
		acme.Service("acme", "wojtek@exw.co", acme.Pebble("10.101.0.12"),
			acme.Waves("10.101.0.3"),
			acme.Domains("test.example.local"),
		),
	),
)
