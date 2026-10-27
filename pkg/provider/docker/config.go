// Package docker builds routers, services and middlewares from labels of
// Docker containers or Swarm services and applies them to the running server
// as the "docker" configuration layer.
package docker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

type Config struct {
	// Enabled starts the docker provider.
	Enabled bool `cfg:"enabled"`
	// Endpoint of the Docker API: unix:///var/run/docker.sock, tcp://host:2376
	// or http(s)://host:port. Empty uses DOCKER_HOST, then the default socket.
	Endpoint string `cfg:"endpoint"`
	// APIVersion pins the API version like "1.47", empty uses the daemon's
	// version.
	APIVersion string `cfg:"api_version"`
	// TLS configures a tcp:// or https:// endpoint.
	TLS *TLS `cfg:"tls"`

	// Swarm reads Swarm services and tasks instead of containers. Turna must
	// talk to a manager node.
	Swarm Swarm `cfg:"swarm"`

	// LabelPrefix of the labels read, default is "turna". Different prefixes
	// let several Turna instances share a Docker host.
	LabelPrefix string `cfg:"label_prefix"`
	// ExposedByDefault serves containers without the <prefix>.enable label;
	// <prefix>.enable=false still opts out.
	ExposedByDefault bool `cfg:"exposed_by_default"`
	// Network is the default network used to reach containers that are
	// attached to several networks. The <prefix>.network label overrides it.
	Network string `cfg:"network"`

	HTTP Protocol `cfg:"http"`
	TCP  Protocol `cfg:"tcp"`
	UDP  Protocol `cfg:"udp"`

	// RefreshInterval re-reads the state besides events, default is 30s
	// (10s in swarm mode, task changes have no events).
	RefreshInterval time.Duration `cfg:"refresh_interval"`
	// Debounce collects changes before applying, default is 200ms.
	Debounce time.Duration `cfg:"debounce"`
}

// Protocol limits what labels may configure for one router kind.
type Protocol struct {
	// EntryPoints labels may attach routers to. For http an empty list allows
	// every entrypoint and routers without entrypoints use this list. For tcp
	// and udp an empty list disables routers from labels, as a stream router
	// takes over the whole entrypoint.
	EntryPoints []string `cfg:"entrypoints"`
	// Middlewares are the middleware types labels may define. Empty uses safe
	// defaults, ["*"] allows every type.
	Middlewares []string `cfg:"middlewares"`
	// StaticMiddlewares are the middlewares of other layers (the static
	// configuration) labels may reference by name, like a shared auth
	// middleware. Empty allows none, ["*"] allows every middleware.
	StaticMiddlewares []string `cfg:"static_middlewares"`
}

type TLS struct {
	CA                 string `cfg:"ca"`
	Cert               string `cfg:"cert"`
	Key                string `cfg:"key"`
	InsecureSkipVerify bool   `cfg:"insecure_skip_verify"`
}

type Swarm struct {
	Enabled bool `cfg:"enabled"`
	// UseVIP proxies to the virtual IP of the service instead of the task
	// addresses. Turna's load balancing and health checks then see a single
	// upstream.
	UseVIP bool `cfg:"use_vip"`
}

// Default middleware types allowed in labels. They only transform or filter
// traffic; types that read files, loaded data or credentials of Turna, or
// open proxies, need an explicit allow.
var (
	DefaultHTTPMiddlewares = []string{
		"add_prefix", "strip_prefix", "regex_path", "headers", "cors", "basic_auth",
		"block", "gzip", "compress", "decompress", "rate_limit", "redirection",
		"request_id", "access_log", "log", "url", "csrf", "ip_filter", "body_limit", "timeout",
		"circuit_breaker", "concurrency_limit", "real_ip", "secure_headers",
	}
	DefaultTCPMiddlewares = []string{
		"ip_allow_list", "ip_deny_list", "conn_limit", "rate_limit", "idle_timeout",
		"bandwidth_limit", "proxy_protocol", "log", "block", "circuit_breaker", "sni_allow_list",
	}
	DefaultUDPMiddlewares = []string{
		"ip_allow_list", "ip_deny_list", "rate_limit", "log", "block", "session_limit", "bandwidth_limit",
	}
)

const (
	defaultEndpoint    = "unix:///var/run/docker.sock"
	defaultLabelPrefix = "turna"
)

var (
	labelPrefixRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(\.[A-Za-z0-9][A-Za-z0-9_-]*)*$`)
	apiVersionRe  = regexp.MustCompile(`^1\.[0-9]+$`)
)

func (c *Config) validate() error {
	if !labelPrefixRe.MatchString(c.labelPrefix()) {
		return fmt.Errorf("label_prefix %q is not valid", c.LabelPrefix)
	}

	if c.APIVersion != "" && !apiVersionRe.MatchString(c.APIVersion) {
		return fmt.Errorf("api_version %q is not valid, use like 1.47", c.APIVersion)
	}

	if c.TLS != nil && (c.TLS.Cert == "") != (c.TLS.Key == "") {
		return errors.New("tls cert and key must be set together")
	}

	return nil
}

func (c *Config) endpoint() string {
	if c.Endpoint != "" {
		return c.Endpoint
	}

	if v := os.Getenv("DOCKER_HOST"); v != "" {
		return v
	}

	return defaultEndpoint
}

// tls returns the TLS configuration; without one, DOCKER_TLS_VERIFY and
// DOCKER_CERT_PATH are used like the docker CLI.
func (c *Config) tls() *TLS {
	if c.TLS != nil || os.Getenv("DOCKER_TLS_VERIFY") == "" {
		return c.TLS
	}

	dir := os.Getenv("DOCKER_CERT_PATH")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return &TLS{}
		}

		dir = filepath.Join(home, ".docker")
	}

	out := &TLS{}
	if _, err := os.Stat(filepath.Join(dir, "ca.pem")); err == nil {
		out.CA = filepath.Join(dir, "ca.pem")
	}

	cert, key := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if _, err := os.Stat(cert); err == nil {
		if _, err := os.Stat(key); err == nil {
			out.Cert, out.Key = cert, key
		}
	}

	return out
}

func (c *Config) labelPrefix() string {
	if c.LabelPrefix == "" {
		return defaultLabelPrefix
	}

	return c.LabelPrefix
}

func (c *Config) refreshInterval() time.Duration {
	if c.RefreshInterval > 0 {
		return c.RefreshInterval
	}

	if c.Swarm.Enabled {
		return 10 * time.Second
	}

	return 30 * time.Second
}

func (c *Config) debounce() time.Duration {
	if c.Debounce <= 0 {
		return 200 * time.Millisecond
	}

	return c.Debounce
}

// allowsMiddleware reports whether labels may define a middleware of typ.
func (p Protocol) allowsMiddleware(typ string, defaults []string) bool {
	list := p.Middlewares
	if len(list) == 0 {
		list = defaults
	}

	return slices.Contains(list, "*") || slices.Contains(list, typ)
}

// allowsStatic reports whether labels may reference the middleware name of
// another layer.
func (p Protocol) allowsStatic(name string) bool {
	return slices.Contains(p.StaticMiddlewares, "*") || slices.Contains(p.StaticMiddlewares, name)
}

func (c *Config) protocol(kind string) Protocol {
	switch kind {
	case "tcp":
		return c.TCP
	case "udp":
		return c.UDP
	default:
		return c.HTTP
	}
}
