package astro

// Config represents astro app configuration.
type Config struct {
	// EnvVars sets environment variables inside astro app container.
	EnvVars map[string]string
}

// Configurator defines function setting the astro app configuration.
type Configurator func(config *Config)

// EnvVar sets environment variable inside astro app container.
func EnvVar(name, value string) Configurator {
	return func(config *Config) {
		config.EnvVars[name] = value
	}
}
