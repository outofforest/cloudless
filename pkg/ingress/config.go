package ingress

// Config defines configuration of HTTP HTTPIngress.
type Config struct {
	// Targets defines target registrations for endpoint.
	Targets map[EndpointID][]Target

	// Endpoints defines HTTP endpoints inside HTTPIngress.
	Endpoints map[EndpointID]Endpoint
}

// EndpointID is an id of HTTPIngress endpoint.
type EndpointID string

// Target represents target registration.
type Target struct {
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

// Endpoint describes configuration of http HTTPIngress endpoint.
type Endpoint struct {
	// Path is a prefix service is available under.
	Path string

	// HTTPSMode defines how https traffic is handled.
	HTTPSMode HTTPSMode

	// PlainBindings specify endpoints in form <ip>:<port> which HTTPIngress binds to for http traffic.
	PlainBindings []string

	// SecureBindings specify endpoints in form <ip>:<port> which HTTPIngress binds to for https traffic.
	SecureBindings []string

	// AllowedDomains are domains requests are accepted for.
	AllowedDomains []string

	// AllowedMethods are allowed http methods.
	AllowedMethods []string

	// AllowWebsockets enables websocket connections.
	AllowWebsockets bool

	// RemoveWWWPrefix causes redirection to URL with `www.` prefix removed.
	RemoveWWWPrefix bool

	// AddSlashToDirs redirects to the path with slash (/) added at the end if there is no dot (.) in last segment.
	AddSlashToDirs bool
}
