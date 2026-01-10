package smtp

// Configurator configures SMTP service.
type Configurator func(c *Config)

// Config is the configuration of SMTP service.
type Config struct {
	Port         int
	AllowedHosts []string
}

// AllowedHostnames defines hostnames accepted by the SMTP service.
func AllowedHostnames(hostnames ...string) Configurator {
	return func(c *Config) {
		c.AllowedHosts = append(c.AllowedHosts, hostnames...)
	}
}
