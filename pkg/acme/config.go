package acme

import (
	"strings"
	"time"

	"github.com/outofforest/cloudless/pkg/pebble"
	"github.com/outofforest/cloudless/pkg/tnet"
	"github.com/outofforest/cloudless/pkg/wave"
)

// Config stores acme configuration.
type Config struct {
	Email       string
	AccountFile string
	CertFile    string
	Directory   DirectoryConfig
	WaveServers []string
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

// Waves adds wave servers to send challenge requests to.
func Waves(waves ...string) Configurator {
	return func(c *Config) {
		for _, w := range waves {
			c.WaveServers = append(c.WaveServers, wave.Address(w))
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
