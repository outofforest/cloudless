package email

import (
	dnsdkim "github.com/outofforest/cloudless/pkg/dns/dkim"
)

// Config stores acme configuration.
type Config struct {
	DNSDKIM []string
}

// Configurator defines function setting the dns configuration.
type Configurator func(c *Config)

// DNSDKIMs adds dns dkim service to connect to when creating DKIM records.
func DNSDKIMs(dnsDKIMs ...string) Configurator {
	return func(c *Config) {
		for _, dnsDKIM := range dnsDKIMs {
			c.DNSDKIM = append(c.DNSDKIM, dnsdkim.Address(dnsDKIM))
		}
	}
}
