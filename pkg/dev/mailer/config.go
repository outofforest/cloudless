package mailer

import (
	"github.com/outofforest/cloudless/pkg/wave"
)

// Config stores acme configuration.
type Config struct {
	Email       string
	DNSServers  []string
	WaveServers []string
}

// Configurator defines function setting the dns configuration.
type Configurator func(c *Config)

// DNS sets dns servers used to resolve MX records.
func DNS(dnses ...string) Configurator {
	return func(c *Config) {
		c.DNSServers = append(c.DNSServers, dnses...)
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
