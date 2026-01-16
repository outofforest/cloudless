package prometheus

import (
	"strings"

	"github.com/pkg/errors"
)

// Config is the config of prometheus.
type Config struct {
	Targets []TargetConfig
}

// TargetConfig configures target to scrape metrics from.
type TargetConfig struct {
	Scheme  string
	Address string
}

// Configurator is the function configuring the prometheus.
type Configurator func(c *Config)

// Targets adds targets to scrape.
func Targets(targets ...string) Configurator {
	const (
		http  = "http://"
		https = "https://"
	)

	return func(c *Config) {
		scheme := "http"
		for _, t := range targets {
			switch {
			case strings.HasPrefix(http, t):
				t = strings.TrimPrefix(t, http)
			case strings.HasPrefix(https, t):
				t = strings.TrimPrefix(t, https)
				scheme = "https"
			case strings.Contains(t, "://"):
				panic(errors.Errorf("invalid target %q", t))
			}
			c.Targets = append(c.Targets, TargetConfig{
				Scheme:  scheme,
				Address: t,
			})
		}
	}
}
