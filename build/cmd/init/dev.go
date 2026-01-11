package main

import (
	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/busybox"
	"github.com/outofforest/cloudless/pkg/container"
	"github.com/outofforest/cloudless/pkg/dev/mailer"
	"github.com/outofforest/cloudless/pkg/dev/smtp"
	"github.com/outofforest/cloudless/pkg/dev/webmail"
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/shield"
	"github.com/outofforest/cloudless/pkg/ssh"
)

var HostDev = Join(
	Host("dev",
		MountPersistentBase("vda"),
		Network("02:00:00:00:00:04", "eth0", Master("igw")),
		Gateway("10.101.0.1"),
		Bridge("igw", "02:00:00:00:03:01", IPs("10.101.0.11/24")),
		container.New("pebble",
			container.Network("igw", "vpebble", "02:00:00:00:03:02"),
		),
		container.New("smtp",
			container.Network("igw", "vsmtp", "02:00:00:00:03:03"),
		),
		container.New("webmail",
			container.Network("igw", "vwebmail", "02:00:00:00:03:04"),
		),
		container.New("mailer",
			container.Network("igw", "vmailer", "02:00:00:00:03:05"),
		),
		container.New("busybox",
			container.Network("igw", "vbusybox", "02:00:00:00:03:06"),
		),
	),
	Container("pebble",
		Network("02:00:00:00:03:02", "igw", IPs("10.101.0.12/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", pebble.Port),
		pebble.Container("pebble", "10.101.0.4:53"),
	),
	Container("smtp",
		Network("02:00:00:00:03:03", "igw", IPs("10.101.0.13/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", smtp.SMTPPort),
		shield.Open("tcp4", "igw", smtp.IMAPPort),
		smtp.Service(),
	),
	Container("webmail",
		Network("02:00:00:00:03:04", "igw", IPs("10.101.0.14/24")),
		Gateway("10.101.0.1"),
		shield.Open("tcp4", "igw", webmail.Port),
		webmail.Container("10.101.0.13:25", "10.101.0.13:143"),
	),
	Container("mailer",
		Network("02:00:00:00:03:05", "igw", IPs("10.101.0.15/24")),
		Gateway("10.101.0.1"),
		mailer.Service("mailer", "wojtek@example.local", "mailer.example.local",
			mailer.DNS("10.101.0.4:53"),
			mailer.Waves("10.101.0.3"),
		),
	),
	Container("busybox",
		Network("02:00:00:00:03:06", "igw", IPs("10.101.0.16/24")),
		Gateway("10.101.0.1"),
		busybox.Install(),
		shield.Open("tcp4", "igw", ssh.Port),
		ssh.Service("AAAAC3NzaC1lZDI1NTE5AAAAIEcJvvtOBgTsm3mq3Sg8cjn6Mz/vC9f3k6a89ZOjIyF6"),
	),
)
