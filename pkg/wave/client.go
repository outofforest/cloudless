package wave

// ClientConfig is the wave client config.
type ClientConfig struct {
	MaxMessageSize uint64
	Servers        []string
}

// NewClientConfig creates new wave config fro client.
func NewClientConfig(maxMessageSize uint64, servers ...string) ClientConfig {
	return ClientConfig{
		MaxMessageSize: maxMessageSize,
		Servers:        servers,
	}
}
