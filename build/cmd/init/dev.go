package main

import (
	"net/http"

	. "github.com/outofforest/cloudless" //nolint:staticcheck
	"github.com/outofforest/cloudless/pkg/container"
	containercache "github.com/outofforest/cloudless/pkg/container/cache"
	"github.com/outofforest/cloudless/pkg/dev/smtp"
	"github.com/outofforest/cloudless/pkg/dev/webmail"
	"github.com/outofforest/cloudless/pkg/dns"
	"github.com/outofforest/cloudless/pkg/eye"
	"github.com/outofforest/cloudless/pkg/ingress"
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/shield"
)

var devContainer = BoxFactory(
	dns.DNS(),
	eye.SendMetrics("http://10.255.0.253:81"),
	eye.RemoteLogging("http://10.255.0.253:82"),
	containercache.Mirrors("http://10.0.0.6:81"),
)

var HostDev = Join(
	Host("dev",
		MountPersistentBase("vda"),
		Network("fc:ff:ff:ff:00:01", "igw", IPs("10.255.0.254/24")),
		Gateway("10.255.0.1"),
		shield.Expose("tcp", "10.255.0.254", ingress.PortHTTP, "10.0.0.5", ingress.PortHTTP),
		shield.Expose("tcp", "10.255.0.254", pebble.Port, "10.0.0.2", pebble.Port),
		shield.Expose("tcp", "10.255.0.254", smtp.SMTPPort, "10.0.0.3", smtp.SMTPPort),
		shield.Expose("tcp", "10.255.0.254", containercache.Port, "10.0.0.6", containercache.Port),
		shield.Expose("udp", "10.255.0.254", dns.Port, "10.0.0.7", dns.Port),
		shield.Masquerade("brint", "igw"),
		Bridge("brint", "fc:ff:ff:ff:01:01", IPs("10.0.0.1/24")),
		container.New("pebble",
			container.Network("brint", "vpebble", "fc:ff:ff:ff:01:02"),
		),
		container.New("smtp",
			container.Network("brint", "vsmtp", "fc:ff:ff:ff:01:03"),
		),
		container.New("webmail",
			container.Network("brint", "vwebmail", "fc:ff:ff:ff:01:04"),
		),
		container.New("ingress",
			container.Network("brint", "vingress", "fc:ff:ff:ff:01:05"),
		),
		container.New("cache",
			container.Network("brint", "vcache", "fc:ff:ff:ff:01:06"),
		),
		container.New("dns",
			container.Network("brint", "vdns", "fc:ff:ff:ff:01:07"),
		),
	),
	devContainer("pebble",
		Network("fc:ff:ff:ff:01:02", "igw", IPs("10.0.0.2/24")),
		Gateway("10.0.0.1"),
		shield.Open("tcp4", "igw", pebble.Port),
		pebble.Container("pebble", "10.255.0.254:53"),
	),
	devContainer("smtp",
		Network("fc:ff:ff:ff:01:03", "igw", IPs("10.0.0.3/24")),
		Gateway("10.0.0.1"),
		shield.Open("tcp4", "igw", smtp.SMTPPort),
		shield.Open("tcp4", "igw", smtp.IMAPPort),
		smtp.Service(),
	),
	devContainer("webmail",
		Network("fc:ff:ff:ff:01:04", "igw", IPs("10.0.0.4/24")),
		Gateway("10.0.0.1"),
		shield.Open("tcp4", "igw", webmail.Port),
		webmail.Container("10.0.0.3:25", "10.0.0.3:143"),
	),
	devContainer("ingress",
		Network("fc:ff:ff:ff:01:05", "igw", IPs("10.0.0.5/24")),
		Gateway("10.0.0.1"),
		shield.Open("tcp4", "igw", ingress.PortHTTP),
		ingress.Service(
			ingress.Endpoint("mail",
				ingress.Domains("mail.dev.local"),
				ingress.HTTPS(ingress.HTTPSModeDisabled),
				ingress.Methods(http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete),
				ingress.BodyLimit(10*1024*1024),
				ingress.EnableWebsockets(),
				ingress.PlainBindings("10.0.0.5:80"),
			),
			ingress.Target("mail", "10.0.0.4", webmail.Port, "/"),
		),
	),
	devContainer("cache",
		Network("fc:ff:ff:ff:01:06", "igw", IPs("10.0.0.6/24")),
		Gateway("10.0.0.1"),
		container.AppMount("container-cache"),
		shield.Open("tcp4", "igw", containercache.Port),
		containercache.Service("container-cache", 1),
	),
	devContainer("dns",
		Network("fc:ff:ff:ff:01:07", "igw", IPs("10.0.0.7/24")),
		Gateway("10.0.0.1"),
		shield.Open("udp4", "igw", dns.Port),
		dns.Service(
			dns.Zone("dev.local", "ns1.dev.local", "wojtek@app.local", 1,
				dns.Nameservers("ns1.dev.local"),
				dns.Domain("ns1.dev.local", "10.255.0.254"),
				dns.Domain("mail.dev.local", "10.255.0.254"),
			),
		),
	),
)
