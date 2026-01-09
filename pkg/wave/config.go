package wave

import "github.com/outofforest/wave"

// Config stores wave configuration.
type Config struct {
	MaxMessageSize uint64
	Servers        []string
}

// Configurator defines function setting the dns configuration.
type Configurator func(c *wave.ServerConfig)

// Servers adds servers to the mesh.
func Servers(servers ...string) Configurator {
	return func(c *wave.ServerConfig) {
		c.Servers = append(c.Servers, servers...)
	}
}
