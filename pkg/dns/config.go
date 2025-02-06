package dns

import (
	"net"

	"github.com/outofforest/cloudless/pkg/parse"
)

// Config stores dns configuration.
type Config struct {
	Zones      map[string]ZoneConfig
	ForwardTo  []net.IP
	ForwardFor []net.IPNet
	EnableACME bool
}

// ZoneConfig stores dns zone configuration.
type ZoneConfig struct {
	// Domain is the domain name configured by zone
	Domain string

	// SerialNumber is incremented whenever zone is changed
	SerialNumber uint32

	// MainNameserver is the address of the main DNS server
	MainNameserver string

	// Email is the email address of zone manager
	Email string

	// Nameservers is the list of nameservers for the zone
	Nameservers []string

	// Domains map domains to IP addresses
	Domains map[string][]net.IP

	// Aliases map one domain to another
	Aliases map[string]AliasConfig

	// MailExchanges specifies mail servers.
	MailExchanges map[string]uint16

	// Texts stores values of TXT records.
	Texts map[string][]string
}

// AliasConfig stores configuration of CNAME alias.
type AliasConfig struct {
	Target  string
	QueryID uint64
}

type (
	// Configurator defines function setting the dns configuration.
	Configurator func(n *Config)

	// ZoneConfigurator defines function setting the dns zone configuration.
	ZoneConfigurator func(n *ZoneConfig)
)

// Zone creates new DNS zone.
func Zone(domain, nameserver, email string, serialNumber uint32, configurators ...ZoneConfigurator) Configurator {
	return func(c *Config) {
		zoneConfig := ZoneConfig{
			Domain:         domain,
			SerialNumber:   serialNumber,
			MainNameserver: nameserver,
			Email:          email,
			Domains:        map[string][]net.IP{},
			Aliases:        map[string]AliasConfig{},
			MailExchanges:  map[string]uint16{},
			Texts:          map[string][]string{},
		}

		for _, configurator := range configurators {
			configurator(&zoneConfig)
		}

		c.Zones[domain] = zoneConfig
	}
}

// Nameservers add nameservers to the zone.
func Nameservers(nameservers ...string) ZoneConfigurator {
	return func(c *ZoneConfig) {
		c.Nameservers = append(c.Nameservers, nameservers...)
	}
}

// Domain adds A records to the zone.
func Domain(domain string, ips ...string) ZoneConfigurator {
	parsedIPs := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		parsedIPs = append(parsedIPs, parse.IP4(ip))
	}
	return func(c *ZoneConfig) {
		c.Domains[domain] = append(c.Domains[domain], parsedIPs...)
	}
}

// Alias adds CNAME record to the zone.
func Alias(from, to string) ZoneConfigurator {
	return func(c *ZoneConfig) {
		c.Aliases[from] = AliasConfig{Target: to}
	}
}

// MailExchange adds MX record to the zone.
func MailExchange(domain string, priority uint16) ZoneConfigurator {
	return func(c *ZoneConfig) {
		c.MailExchanges[domain] = priority
	}
}

// Text adds TXT record to the zone.
func Text(domain string, values ...string) ZoneConfigurator {
	return func(c *ZoneConfig) {
		c.Texts[domain] = append(c.Texts[domain], values...)
	}
}

// ForwardTo sets DNS servers for forwarding.
func ForwardTo(servers ...string) Configurator {
	parsedIPs := make([]net.IP, 0, len(servers))
	for _, ip := range servers {
		parsedIPs = append(parsedIPs, parse.IP4(ip))
	}
	return func(c *Config) {
		c.ForwardTo = append(c.ForwardTo, parsedIPs...)
	}
}

// ForwardFor sets networks allowed to make forwarded DNS requests.
func ForwardFor(networks ...string) Configurator {
	parsedNetworks := make([]net.IPNet, 0, len(networks))
	for _, n := range networks {
		parsedNetworks = append(parsedNetworks, parse.IPNet4(n))
	}
	return func(c *Config) {
		c.ForwardFor = append(c.ForwardFor, parsedNetworks...)
	}
}

// ACME enables service required to authenticate ACME certificate requests.
func ACME() Configurator {
	return func(c *Config) {
		c.EnableACME = true
	}
}
