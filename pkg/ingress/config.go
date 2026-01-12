package ingress

import "github.com/outofforest/cloudless/pkg/wave"

// Config defines configuration of HTTP HTTPIngress.
type Config struct {
	// WaveServers are addresses of wave servers.
	WaveServers []string

	// Targets defines target registrations for endpoint.
	Targets map[EndpointID][]TargetConfig

	// Endpoints defines HTTP endpoints inside HTTPIngress.
	Endpoints map[EndpointID]EndpointConfig
}

// EndpointID is an id of HTTPIngress endpoint.
type EndpointID string

// TargetConfig represents target registration.
type TargetConfig struct {
	Host string
	Port uint16
	Path string
}

// HTTPSMode defines how https traffic is handled by http HTTPIngress endpoint.
type HTTPSMode string

const (
	// HTTPSModeDisabled causes HTTPIngress to work in http mode only.
	HTTPSModeDisabled HTTPSMode = "disabled"

	// HTTPSModeOptional causes HTTPIngress to work in both http and https modes.
	HTTPSModeOptional HTTPSMode = "optional"

	// HTTPSModeRedirect causes HTTPIngress to redirect client from http to https.
	HTTPSModeRedirect HTTPSMode = "redirect"

	// HTTPSModeOnly causes HTTPIngress to run only https endpoint, http just won't respond at all.
	HTTPSModeOnly HTTPSMode = "only"
)

// EndpointConfig describes configuration of http HTTPIngress endpoint.
type EndpointConfig struct {
	// Path is a prefix service is available under.
	Path string

	// HTTPSMode defines how https traffic is handled.
	HTTPSMode HTTPSMode

	// PlainBindings specify endpoints in form <ip>:<port> which HTTPIngress binds to for http traffic.
	PlainBindings []string

	// TLSBindings specify endpoints in form <ip>:<port> which HTTPIngress binds to for https traffic.
	TLSBindings []string

	// AllowedDomains are domains requests are accepted for.
	AllowedDomains []string

	// AllowedMethods are allowed http methods.
	AllowedMethods []string

	// AllowWebsockets enables websocket connections.
	AllowWebsockets bool

	// MaxBodyLength defines maximum size of request body.
	MaxBodyLength int64

	// RemoveWWWPrefix causes redirection to URL with `www.` prefix removed.
	RemoveWWWPrefix bool

	// AddSlashToDirs redirects to the path with slash (/) added at the end if there is no dot (.) in last segment.
	AddSlashToDirs bool
}

// Configurator is the function configuring ingress.
type Configurator func(c *Config)

// Waves adds wave servers to send challenge requests to.
func Waves(waves ...string) Configurator {
	return func(c *Config) {
		for _, w := range waves {
			c.WaveServers = append(c.WaveServers, wave.Address(w))
		}
	}
}

// EndpointConfigurator is the function configuring endpoint.
type EndpointConfigurator func(c *EndpointConfig)

// Target adds target for an endpoint.
func Target(endpointID EndpointID, host string, port uint16, path string) Configurator {
	return func(c *Config) {
		c.Targets[endpointID] = append(c.Targets[endpointID], TargetConfig{
			Host: host,
			Port: port,
			Path: path,
		})
	}
}

// Endpoint adds endpoint to the ingress.
func Endpoint(endpointID EndpointID, configurators ...EndpointConfigurator) Configurator {
	return func(c *Config) {
		config := EndpointConfig{
			Path:      "/",
			HTTPSMode: HTTPSModeOnly,
		}

		for _, configurator := range configurators {
			configurator(&config)
		}
		c.Endpoints[endpointID] = config
	}
}

// Domains sets serviced domains.
func Domains(domains ...string) EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.AllowedDomains = append(c.AllowedDomains, domains...)
	}
}

// Methods sets serviced HTTP methods.
func Methods(methods ...string) EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.AllowedMethods = append(c.AllowedMethods, methods...)
	}
}

// EnableWebsockets enables websockets.
func EnableWebsockets() EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.AllowWebsockets = true
	}
}

// RemoveWWW causes redirection of www.* domains to non-www ones.
func RemoveWWW() EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.RemoveWWWPrefix = true
	}
}

// AddSlash causes redirection to slash-suffixed URL if URL does not end with slash or dotted (.sth) suffix.
func AddSlash() EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.AddSlashToDirs = true
	}
}

// HTTPS configures mode of https operation.
func HTTPS(httpsMode HTTPSMode) EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.HTTPSMode = httpsMode
	}
}

// Path sets path for the endpoint.
func Path(path string) EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.Path = path
	}
}

// TLSBindings configures socket bindings for HTTPS.
func TLSBindings(bindings ...string) EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.TLSBindings = append(c.TLSBindings, bindings...)
	}
}

// PlainBindings configures socket bindings for HTTP.
func PlainBindings(bindings ...string) EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.PlainBindings = append(c.PlainBindings, bindings...)
	}
}

// BodyLimit sets the maximum allowed size of the request body in bytes.
// Requests exceeding this limit will be rejected.
func BodyLimit(limit int64) EndpointConfigurator {
	return func(c *EndpointConfig) {
		c.MaxBodyLength = limit
	}
}
