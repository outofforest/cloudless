package eye

import "github.com/outofforest/cloudless/pkg/host"

// RemoteLogging configures remote logging.
func RemoteLogging(lokiURL string) host.Configurator {
	return func(c *host.Configuration) error {
		c.RemoteLogging(lokiURL)
		return nil
	}
}
