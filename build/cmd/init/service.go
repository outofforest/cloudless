package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/acme"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/dev"
	"github.com/outofforest/cloudless/pkg/dev/mailer"
	"github.com/outofforest/cloudless/pkg/dns"
	"github.com/outofforest/cloudless/pkg/shield"
	"github.com/outofforest/cloudless/pkg/wave"
)

var HostService = Join(
	Host("service",
		MountPersistentBase("vda"),
		Network("02:00:00:00:00:02", "igw", IPs("10.255.0.2/24")),
		Gateway("10.255.0.1"),
		shield.Expose("udp", "10.255.0.2", dns.Port, "10.0.0.3", dns.Port),
		shield.Masquerade("brint", "igw"),
		Bridge("brint", "02:00:00:00:01:01", IPs("10.0.0.1/24")),
		container.New("wave",
			container.Network("brint", "vwave", "02:00:00:00:01:02"),
		),
		container.New("dns",
			container.Network("brint", "vdns", "02:00:00:00:01:03"),
		),
		container.New("acme",
			container.Network("brint", "vacme", "02:00:00:00:01:04"),
		),
		container.New("mailer",
			container.Network("brint", "vmailer", "02:00:00:00:01:05"),
		),
	),
	Container("wave",
		Network("02:00:00:00:01:02", "igw", IPs("10.0.0.2/24")),
		Gateway("10.0.0.1"),
		shield.Open("tcp4", "igw", wave.Port),
		wave.Service(20*1024),
	),
	Container("dns",
		Network("02:00:00:00:01:03", "igw", IPs("10.0.0.3/24")),
		Gateway("10.0.0.1"),
		shield.Open("udp4", "igw", dns.Port),
		dns.Service(
			dns.Waves("10.0.0.2"),
			dns.ACME(),
			dns.DKIM(),
			dns.Zone("app.local", "ns1.app.local", "wojtek@app.local", 1,
				dns.Nameservers("ns1.app.local"),
				dns.MailExchange("smtp.app.local", 10),
				dns.Domain("ns1.app.local", "10.255.0.2"),
				dns.Domain("smtp.app.local", dev.SMTPAddr),
				dns.Text("_dmarc.app.local", "v=DMARC1;p=quarantine"),
				dns.Text("app.local", "v=spf1 a:mailer.app.local ~all"),
				dns.Domain("app.local", "10.255.0.2"),
				dns.Domain("mailer.app.local", "10.255.0.2"),
			),
		),
	),
	Container("acme",
		Network("02:00:00:00:01:04", "igw", IPs("10.0.0.4/24")),
		Gateway("10.0.0.1"),
		container.AppMount("acme"),
		acme.Service("acme", "wojtek@exw.co", acme.Pebble(dev.PebbleAddr),
			acme.Waves("10.0.0.2"),
			acme.Domains("app.local", "*.app.local"),
		),
	),
	Container("mailer",
		Network("02:00:00:00:01:05", "igw", IPs("10.0.0.5/24")),
		Gateway("10.0.0.1"),
		mailer.Service("mailer", "wojtek@app.local", "mailer.app.local",
			mailer.Waves("10.0.0.2"),
			mailer.DNS("10.0.0.3:53"),
		),
	),
)
