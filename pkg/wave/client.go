package wave

// ClientConfig is the wave client config.
type ClientConfig struct {
	MaxMessageSize uint64
	Servers        []string
}

// NewClientConfig creates new wave config fro client.
func NewClientConfig(maxMessageSize uint64, servers ...string) ClientConfig {
	c := ClientConfig{
		MaxMessageSize: maxMessageSize,
		Servers:        make([]string, 0, len(servers)),
	}
	for _, s := range servers {
		c.Servers = append(c.Servers, Address(s))
	}
	return c
}
