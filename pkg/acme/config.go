package acme

import (
	"strings"
	"time"

	dnsacme "github.com/outofforest/cloudless/pkg/dns/acme"
	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/tnet"
)

// Config stores acme configuration.
type Config struct {
	AccountFile string
	CertFile    string
	Directory   DirectoryConfig
	DNSACME     []string
	Domains     []string
}

// Configurator defines function setting the dns configuration.
type Configurator func(c *Config)

// DirectoryConfig is the config of ACME directory service.
type DirectoryConfig struct {
	Name          string
	Provider      string
	DirectoryURL  string
	Insecure      bool
	RenewDuration time.Duration
}

var (
	// LetsEncrypt is the LetsEncrypt production config.
	LetsEncrypt = DirectoryConfig{
		Name:          "lestsencrypt",
		Provider:      "letsencrypt.org",
		DirectoryURL:  "https://acme-v02.api.letsencrypt.org/directory",
		RenewDuration: time.Hour * 24 * 29, // 29 days
	}

	// LetsEncryptStaging is the LetsEncrypt staging config.
	LetsEncryptStaging = DirectoryConfig{
		Name:          "lestsencrypt-staging",
		Provider:      "letsencrypt.org",
		DirectoryURL:  "https://acme-staging-v02.api.letsencrypt.org/directory",
		RenewDuration: 30 * time.Minute,
	}
)

// Pebble returns directory config for pebble.
func Pebble(host string) DirectoryConfig {
	return DirectoryConfig{
		Name:          "pebble",
		Provider:      "pebble",
		DirectoryURL:  tnet.JoinScheme("https", host, pebble.Port) + "/dir",
		Insecure:      true,
		RenewDuration: 3 * time.Minute,
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
