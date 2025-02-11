package acme

import (
	"strings"

	dnsacme "github.com/outofforest/cloudless/pkg/dns/acme"
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/tnet"
)

// Config stores acme configuration.
type Config struct {
	Directory DirectoryConfig
	DNSACME   []string
	Domains   []string
}

// Configurator defines function setting the dns configuration.
type Configurator func(c *Config)

// DirectoryConfig is the config of ACME directory service.
type DirectoryConfig struct {
	Provider     string
	DirectoryURL string
	Insecure     bool
}

var (
	// LetsEncrypt is the LetsEncrypt production config.
	LetsEncrypt = DirectoryConfig{
		Provider:     "letsencrypt.org",
		DirectoryURL: "https://acme-v02.api.letsencrypt.org/directory",
	}

	// LetsEncryptStaging is the LetsEncrypt staging config.
	LetsEncryptStaging = DirectoryConfig{
		Provider:     "letsencrypt.org",
		DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
	}
)

// Pebble returns directory config for pebble.
func Pebble(host string) DirectoryConfig {
	return DirectoryConfig{
		Provider:     "pebble",
		DirectoryURL: tnet.JoinScheme("https", host, pebble.Port) + "/dir",
		Insecure:     true,
	}
}

// DNSACMEs adds dns acme service to connect to when creating challenges.
func DNSACMEs(dnsACMEs ...string) Configurator {
	return func(c *Config) {
		for _, dnsACME := range dnsACMEs {
			c.DNSACME = append(c.DNSACME, tnet.Join(dnsACME, dnsacme.Port))
		}
	}
}

// Domains adds domains to issue certificate for.
func Domains(domains ...string) Configurator {
	return func(c *Config) {
		for _, d := range domains {
			c.Domains = append(c.Domains, strings.ToLower(d))
		}
	}
}
