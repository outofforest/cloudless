package wave

import (
	"github.com/outofforest/resonance"
)

// Config is the wave client config.
type Config struct {
	CA             *resonance.CA
	MaxMessageSize uint64
	Servers        []string
}

// NewConfig creates new wave config.
func NewConfig(ca *resonance.CA, maxMessageSize uint64, servers ...string) Config {
	c := Config{
		CA:             ca,
		MaxMessageSize: maxMessageSize,
		Servers:        make([]string, 0, len(servers)),
	}
	for _, s := range servers {
		c.Servers = append(c.Servers, Address(s))
	}
	return c
}
