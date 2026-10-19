package docker

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server"
	shttp "github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
	"github.com/rakunlabs/turna/pkg/server/netlb"
	"github.com/rakunlabs/turna/pkg/server/tcp"
	tcpblock "github.com/rakunlabs/turna/pkg/server/tcp/middleware/block"
	tcplb "github.com/rakunlabs/turna/pkg/server/tcp/middleware/loadbalancer"
	"github.com/rakunlabs/turna/pkg/server/udp"
	udpblock "github.com/rakunlabs/turna/pkg/server/udp/middleware/block"
	udplb "github.com/rakunlabs/turna/pkg/server/udp/middleware/loadbalancer"
)

// group is a set of containers configured by the same labels: the replicas of
// a compose service, a swarm service or a single container.
type group struct {
	Name string
	// Created orders groups when they claim the same route, older wins.
	Created time.Time
	Labels  map[string]string
	// Targets are the addresses (IPs) of the members that receive traffic.
	Targets []string
	// Ports exposed by the members by protocol (tcp, udp), used when a
	// service has no port label.
	Ports map[string][]int
	// Err is set when the members cannot be reached, the group is skipped.
	Err error
	// HostNetwork is set when members use the host network, Swarm for swarm
	// services; both only change error messages.
	HostNetwork bool
	Swarm       bool
}

// labels decoded below the prefix
type labelConfig struct {
	Enable  *bool  `cfg:"enable"`
	Network string `cfg:"network"`

	HTTP struct {
		Routers     map[string]httpRouter  `cfg:"routers"`
		Services    map[string]httpService `cfg:"services"`
		Middlewares map[string]any         `cfg:"middlewares"`
	} `cfg:"http"`

	TCP struct {
		Routers     map[string]streamRouter `cfg:"routers"`
		Services    map[string]tcpService   `cfg:"services"`
		Middlewares map[string]any          `cfg:"middlewares"`
	} `cfg:"tcp"`

	UDP struct {
		Routers     map[string]streamRouter `cfg:"routers"`
		Services    map[string]udpService   `cfg:"services"`
		Middlewares map[string]any          `cfg:"middlewares"`
	} `cfg:"udp"`
}

type httpRouter struct {
	// Host names, a router is created for each. Empty matches every host.
	Host        []string `cfg:"host"`
	Path        []string `cfg:"path"`
	EntryPoints []string `cfg:"entrypoints"`
	Middlewares []string `cfg:"middlewares"`
	TLS         bool     `cfg:"tls"`
	Service     string   `cfg:"service"`
}

type httpService struct {
	Port int `cfg:"port"`
	// Scheme is http or https, default is http.
	Scheme             string `cfg:"scheme"`
	H2C                bool   `cfg:"h2c"`
	PassHostHeader     *bool  `cfg:"pass_host_header"`
	InsecureSkipVerify bool   `cfg:"insecure_skip_verify"`
	// ServerName is sent as SNI to https upstreams and verified.
	ServerName         string                      `cfg:"server_name"`
	Retry              *int                        `cfg:"retry"`
	Strategy           string                      `cfg:"strategy"`
	Sticky             *service.Sticky             `cfg:"sticky"`
	HealthCheck        *service.HealthCheck        `cfg:"health_check"`
	PassiveHealthCheck *service.PassiveHealthCheck `cfg:"passive_health_check"`
}

type streamRouter struct {
	EntryPoints []string `cfg:"entrypoints"`
	Middlewares []string `cfg:"middlewares"`
	Service     string   `cfg:"service"`
}

type tcpService struct {
	Port                 int                       `cfg:"port"`
	Strategy             string                    `cfg:"strategy"`
	Retry                int                       `cfg:"retry"`
	DialTimeout          time.Duration             `cfg:"dial_timeout"`
	DisableNagle         bool                      `cfg:"disable_nagle"`
	ProxyProtocol        bool                      `cfg:"proxy_protocol"`
	ProxyProtocolVersion int                       `cfg:"proxy_protocol_version"`
	HealthCheck          *netlb.HealthCheck        `cfg:"health_check"`
	PassiveHealthCheck   *netlb.PassiveHealthCheck `cfg:"passive_health_check"`
}

type udpService struct {
	Port               int                       `cfg:"port"`
	Strategy           string                    `cfg:"strategy"`
	SessionTimeout     time.Duration             `cfg:"session_timeout"`
	MaxSessions        int                       `cfg:"max_sessions"`
	HealthCheck        *udplb.HealthCheck        `cfg:"health_check"`
	PassiveHealthCheck *netlb.PassiveHealthCheck `cfg:"passive_health_check"`
}

const defaultRetry = 2

var (
	httpMiddlewareTypes = cfgKeys(reflect.TypeFor[shttp.HTTPMiddleware]())
	tcpMiddlewareTypes  = cfgKeys(reflect.TypeFor[tcp.TCPMiddleware]())
	udpMiddlewareTypes  = cfgKeys(reflect.TypeFor[udp.UDPMiddleware]())
)

// readLabels decodes the labels of a group.
func readLabels(labels map[string]string, prefix string) (*labelConfig, error) {
	tree, err := labelTree(labels, prefix)
	if err != nil {
		return nil, err
	}

	out := &labelConfig{}
	if err := decode(tree, out); err != nil {
		return nil, err
	}

	return out, nil
}

// enabled reports whether a container or service with labels is served.
func enabled(labels map[string]string, prefix string, byDefault bool) bool {
	v, ok := labels[prefix+".enable"]
	if !ok {
		return byDefault
	}

	b, err := strconv.ParseBool(strings.TrimSpace(v))

	return err == nil && b
}

// fragment is the configuration of one group.
type fragment struct {
	server *server.Server
	// warnings are problems that dropped a part of the group
	warnings []error
}

func newServer() *server.Server {
	return &server.Server{
		HTTP: shttp.HTTP{
			Routers:     make(map[string]shttp.Router),
			Middlewares: make(map[string]shttp.HTTPMiddleware),
		},
		TCP: tcp.TCP{
			Routers:     make(map[string]tcp.Router),
			Middlewares: make(map[string]tcp.TCPMiddleware),
		},
		UDP: udp.UDP{
			Routers:     make(map[string]udp.Router),
			Middlewares: make(map[string]udp.UDPMiddleware),
		},
	}
}

// claims tracks what earlier groups and other layers use, so a group that
// would make the whole configuration invalid is rejected on its own.
type claims struct {
	// networks of the entrypoints by name
	entrypoints map[string]string
	// middleware names of other layers by kind
	middlewares map[string]map[string]struct{}

	// http routes by host and path, the value is the entrypoints
	http map[[2]string][][]string
	// hosts of other layers with their entrypoints; containers cannot add
	// paths to them, which would bypass the middlewares of those routers
	reserved map[string][][]string
	// tls mode of entrypoints used by http routers
	secure map[string]bool
	// entrypoints other layers use with both tls modes
	mixed map[string]struct{}
	// entrypoints used by tcp and udp routers, the value is the router
	tcp map[string]string
	udp map[string]string
}

func newClaims(others *server.Server) *claims {
	c := &claims{
		entrypoints: make(map[string]string),
		middlewares: map[string]map[string]struct{}{"http": {}, "tcp": {}, "udp": {}},
		http:        make(map[[2]string][][]string),
		reserved:    make(map[string][][]string),
		secure:      make(map[string]bool),
		mixed:       make(map[string]struct{}),
		tcp:         make(map[string]string),
		udp:         make(map[string]string),
	}

	if others == nil {
		return c
	}

	for name, e := range others.EntryPoints {
		c.entrypoints[name] = e.Network
	}

	for name := range others.HTTP.Middlewares {
		if !isOwned(name) {
			c.middlewares["http"][name] = struct{}{}
		}
	}
	for name := range others.TCP.Middlewares {
		if !isOwned(name) {
			c.middlewares["tcp"][name] = struct{}{}
		}
	}
	for name := range others.UDP.Middlewares {
		if !isOwned(name) {
			c.middlewares["udp"][name] = struct{}{}
		}
	}

	for name, r := range others.TCP.Routers {
		if !isOwned(name) {
			for _, e := range r.EntryPoints {
				c.tcp[e] = name
			}
		}
	}

	for name, r := range others.UDP.Routers {
		if !isOwned(name) {
			for _, e := range r.EntryPoints {
				c.udp[e] = name
			}
		}
	}

	for _, name := range keys(others.HTTP.Routers) {
		r := others.HTTP.Routers[name]
		if isOwned(name) {
			continue
		}

		eps := r.EntryPoints
		if len(eps) == 0 {
			eps = c.httpEntrypoints()
		}

		for _, e := range eps {
			// routers of other layers mixing tls modes on an entrypoint
			// leave it to them
			if secure, ok := c.secure[e]; ok && secure != (r.TLS != nil) {
				c.mixed[e] = struct{}{}
			}

			c.secure[e] = r.TLS != nil
		}

		host := normalizeHost(r.Host)
		c.reserved[host] = append(c.reserved[host], eps)
	}

	return c
}

func isPacket(network string) bool {
	return strings.HasPrefix(network, "udp")
}

// httpEntrypoints are the stream entrypoints without a tcp router.
func (c *claims) httpEntrypoints() []string {
	out := make([]string, 0, len(c.entrypoints))
	for name, network := range c.entrypoints {
		if _, ok := c.tcp[name]; !ok && !isPacket(network) {
			out = append(out, name)
		}
	}

	slices.Sort(out)

	return out
}

// checkEntrypoint reports whether a router of kind can use the entrypoint.
func (c *claims) checkEntrypoint(kind, name string) error {
	network, ok := c.entrypoints[name]
	if !ok {
		return fmt.Errorf("entrypoint %s does not exist", name)
	}

	if (kind == "udp") != isPacket(network) {
		return fmt.Errorf("entrypoint %s is not a %s entrypoint", name, kind)
	}

	return nil
}

func overlap(a, b []string) bool {
	return slices.ContainsFunc(a, func(e string) bool { return slices.Contains(b, e) })
}

// claimHTTP takes the routes of a router or reports why it cannot.
func (c *claims) claimHTTP(host string, paths, entrypoints []string, tls bool) error {
	host = normalizeHost(host)

	for _, e := range entrypoints {
		if err := c.checkEntrypoint("http", e); err != nil {
			return err
		}

		if by, ok := c.tcp[e]; ok {
			return fmt.Errorf("entrypoint %s is used by tcp router %s", e, by)
		}

		if _, ok := c.mixed[e]; ok {
			return fmt.Errorf("entrypoint %s serves both tls and plain http in the static configuration, use another entrypoint", e)
		}

		if secure, ok := c.secure[e]; ok && secure != tls {
			if secure {
				return fmt.Errorf("entrypoint %s serves tls, set tls=true or use another entrypoint", e)
			}

			return fmt.Errorf("entrypoint %s serves plain http, remove tls or use another entrypoint", e)
		}
	}

	for reserved, eps := range c.reserved {
		if (reserved == host || wildcardMatches(reserved, host)) && slices.ContainsFunc(eps, func(e []string) bool { return overlap(e, entrypoints) }) {
			if host == "" {
				return errors.New("routers without host are served by the static configuration, set host")
			}

			return fmt.Errorf("host %s is served by the static configuration", host)
		}
	}

	for _, p := range paths {
		for _, eps := range c.http[[2]string{host, p}] {
			if overlap(eps, entrypoints) {
				if host == "" {
					return fmt.Errorf("path %s is already routed", p)
				}

				return fmt.Errorf("host %s path %s is already routed", host, p)
			}
		}
	}

	for _, e := range entrypoints {
		c.secure[e] = tls
	}

	for _, p := range paths {
		k := [2]string{host, p}
		c.http[k] = append(c.http[k], entrypoints)
	}

	return nil
}

// defaultEntrypoints are the http entrypoints a router without entrypoints
// uses: the ones serving the same tls mode or not used yet. Entrypoints kept
// for tcp routers are skipped.
func (c *claims) defaultEntrypoints(tls bool, streams []string) []string {
	out := make([]string, 0)
	for _, e := range c.httpEntrypoints() {
		if slices.Contains(streams, e) {
			continue
		}

		if _, ok := c.mixed[e]; ok {
			continue
		}

		if secure, ok := c.secure[e]; !ok || secure == tls {
			out = append(out, e)
		}
	}

	return out
}

func (c *claims) claimStream(kind string, entrypoints []string, owner string) error {
	taken := c.tcp
	if kind == "udp" {
		taken = c.udp
	}

	for _, e := range entrypoints {
		if err := c.checkEntrypoint(kind, e); err != nil {
			return err
		}

		if by, ok := taken[e]; ok {
			return fmt.Errorf("entrypoint %s is already used by %s", e, by)
		}

		if kind == "tcp" {
			if _, ok := c.secure[e]; ok {
				return fmt.Errorf("entrypoint %s is used by http routers", e)
			}
		}
	}

	for _, e := range entrypoints {
		taken[e] = owner
	}

	return nil
}

func normalizeHost(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
}

// wildcardMatches reports whether a wildcard host like *.example.com covers
// host, including narrower wildcards like *.a.example.com.
func wildcardMatches(pattern, host string) bool {
	suffix, ok := strings.CutPrefix(pattern, "*")
	if !ok || host == "" {
		return false
	}

	return strings.HasSuffix(host, suffix)
}

const namePrefix = "docker@"

func isOwned(name string) bool {
	return strings.HasPrefix(name, namePrefix)
}

// translator builds the fragment of one group.
type translator struct {
	cfg    *Config
	g      *group
	lc     *labelConfig
	claims *claims
	out    *fragment
}

func (t *translator) name(kind, name string) string {
	return namePrefix + t.g.Name + "@" + kind + "@" + name
}

func (t *translator) warn(format string, args ...any) {
	t.out.warnings = append(t.out.warnings, fmt.Errorf(format, args...))
}

// translate builds the configuration of a group. An error rejects the whole
// group; problems with a single router are warnings.
func translate(cfg *Config, g *group, cl *claims) (*fragment, error) {
	if g.Err != nil {
		return nil, g.Err
	}

	lc, err := readLabels(g.Labels, cfg.labelPrefix())
	if err != nil {
		return nil, err
	}

	if err := validNames(lc); err != nil {
		return nil, err
	}

	t := &translator{cfg: cfg, g: g, lc: lc, claims: cl, out: &fragment{server: newServer()}}

	if err := t.middlewares(); err != nil {
		return nil, err
	}

	t.httpRouters()
	t.tcpRouters()
	t.udpRouters()

	t.pruneServices()

	return t.out, nil
}

// pruneServices drops services of rejected routers, they would run health
// checks without serving traffic.
func (t *translator) pruneServices() {
	s := t.out.server

	pruneUnused(s.HTTP.Middlewares, namePrefix+t.g.Name+"@http-service@", func(yield func([]string) bool) {
		for _, r := range s.HTTP.Routers {
			if !yield(r.Middlewares) {
				return
			}
		}
	})
	pruneUnused(s.TCP.Middlewares, namePrefix+t.g.Name+"@tcp-service@", func(yield func([]string) bool) {
		for _, r := range s.TCP.Routers {
			if !yield(r.Middlewares) {
				return
			}
		}
	})
	pruneUnused(s.UDP.Middlewares, namePrefix+t.g.Name+"@udp-service@", func(yield func([]string) bool) {
		for _, r := range s.UDP.Routers {
			if !yield(r.Middlewares) {
				return
			}
		}
	})
}

func pruneUnused[V any](middlewares map[string]V, prefix string, routers func(yield func([]string) bool)) {
	used := make(map[string]struct{})
	for names := range routers {
		for _, n := range names {
			used[n] = struct{}{}
		}
	}

	for name := range middlewares {
		if _, ok := used[name]; !ok && strings.HasPrefix(name, prefix) {
			delete(middlewares, name)
		}
	}
}

func validNames(lc *labelConfig) error {
	check := func(kind string, names []string) error {
		for _, n := range names {
			if strings.Contains(n, "@") {
				return fmt.Errorf("%s name %q must not contain @", kind, n)
			}
		}

		return nil
	}

	return errors.Join(
		check("http router", keys(lc.HTTP.Routers)),
		check("http service", keys(lc.HTTP.Services)),
		check("http middleware", keys(lc.HTTP.Middlewares)),
		check("tcp router", keys(lc.TCP.Routers)),
		check("tcp service", keys(lc.TCP.Services)),
		check("tcp middleware", keys(lc.TCP.Middlewares)),
		check("udp router", keys(lc.UDP.Routers)),
		check("udp service", keys(lc.UDP.Services)),
		check("udp middleware", keys(lc.UDP.Middlewares)),
	)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return out
}

// middlewares decodes the middlewares defined in labels. An invalid
// middleware rejects the group: routers using it would run without it.
func (t *translator) middlewares() error {
	for _, name := range keys(t.lc.HTTP.Middlewares) {
		var m shttp.HTTPMiddleware
		if err := t.middleware("http", name, t.lc.HTTP.Middlewares[name], httpMiddlewareTypes, t.cfg.HTTP, DefaultHTTPMiddlewares, &m); err != nil {
			return err
		}

		t.out.server.HTTP.Middlewares[t.name("http", name)] = m
	}

	for _, name := range keys(t.lc.TCP.Middlewares) {
		var m tcp.TCPMiddleware
		if err := t.middleware("tcp", name, t.lc.TCP.Middlewares[name], tcpMiddlewareTypes, t.cfg.TCP, DefaultTCPMiddlewares, &m); err != nil {
			return err
		}

		t.out.server.TCP.Middlewares[t.name("tcp", name)] = m
	}

	for _, name := range keys(t.lc.UDP.Middlewares) {
		var m udp.UDPMiddleware
		if err := t.middleware("udp", name, t.lc.UDP.Middlewares[name], udpMiddlewareTypes, t.cfg.UDP, DefaultUDPMiddlewares, &m); err != nil {
			return err
		}

		t.out.server.UDP.Middlewares[t.name("udp", name)] = m
	}

	return nil
}

func (t *translator) middleware(kind, name string, def any, known []string, p Protocol, defaults []string, out any) error {
	typ, body, err := middlewareType(def)
	if err != nil {
		return fmt.Errorf("%s middleware %s: %w", kind, name, err)
	}

	canonical, ok := canonicalType(typ, known)
	if !ok {
		return fmt.Errorf("%s middleware %s: unknown type %s", kind, name, typ)
	}

	if !p.allowsMiddleware(canonical, defaults) {
		return fmt.Errorf("%s middleware %s: type %s is not allowed in labels, see providers.docker.%s.middlewares", kind, name, canonical, kind)
	}

	if err := decode(map[string]any{canonical: body[typ]}, out); err != nil {
		return fmt.Errorf("%s middleware %s: %w", kind, name, err)
	}

	// labels must not make Turna connect to arbitrary addresses
	if m, ok := out.(*shttp.HTTPMiddleware); ok && m.RateLimit != nil && m.RateLimit.Redis != nil {
		return fmt.Errorf("%s middleware %s: rate_limit redis is not allowed in labels", kind, name)
	}

	return nil
}

// resolve maps middleware names of a router to the names of this group, or
// to middlewares of other layers.
func (t *translator) resolve(kind string, names []string, local map[string]any) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := local[n]; ok {
			out = append(out, t.name(kind, n))

			continue
		}

		if _, ok := t.claims.middlewares[kind][n]; !ok {
			return nil, fmt.Errorf("middleware %s is not defined", n)
		}

		if !t.cfg.protocol(kind).allowsStatic(n) {
			return nil, fmt.Errorf("middleware %s is not allowed in labels, see providers.docker.%s.static_middlewares", n, kind)
		}

		out = append(out, n)
	}

	return out, nil
}

// serviceName returns the service of a router: the named one, the only one
// of the group, or "" for the implicit default service.
func serviceName[V any](name string, services map[string]V) (string, error) {
	if name != "" {
		if _, ok := services[name]; !ok {
			return "", fmt.Errorf("service %s is not defined", name)
		}

		return name, nil
	}

	switch len(services) {
	case 0:
		return "", nil
	case 1:
		for k := range services {
			return k, nil
		}
	}

	return "", errors.New("several services are defined, set service")
}

func (t *translator) port(kind, svc string, port int) (int, error) {
	if port < 0 || port > 65535 {
		return 0, fmt.Errorf("%s service %s: port %d is not valid", kind, svc, port)
	}

	if port > 0 {
		return port, nil
	}

	proto := "tcp"
	if kind == "udp" {
		proto = "udp"
	}

	ports := t.g.Ports[proto]

	switch len(ports) {
	case 1:
		return ports[0], nil
	case 0:
		switch {
		case t.g.Swarm:
			return 0, fmt.Errorf("%s service %s: no port is published, swarm only lists published ports, set port", kind, svc)
		case t.g.HostNetwork:
			return 0, fmt.Errorf("%s service %s: host network containers list no ports, set port", kind, svc)
		}

		return 0, fmt.Errorf("%s service %s: no port is exposed, set port", kind, svc)
	default:
		return 0, fmt.Errorf("%s service %s: several ports are exposed, set port", kind, svc)
	}
}

func (t *translator) httpService(name string) (string, error) {
	svc := t.lc.HTTP.Services[name]

	label := name
	if label == "" {
		label = "default"
	}

	mwName := t.name("http-service", label)
	if _, ok := t.out.server.HTTP.Middlewares[mwName]; ok {
		return mwName, nil
	}

	port, err := t.port("http", label, svc.Port)
	if err != nil {
		return "", err
	}

	scheme := strings.ToLower(svc.Scheme)
	switch scheme {
	case "":
		scheme = "http"
	case "http", "https":
	default:
		return "", fmt.Errorf("http service %s: scheme %q is not http or https", label, svc.Scheme)
	}

	if svc.H2C && scheme == "https" {
		return "", fmt.Errorf("http service %s: h2c needs scheme http", label)
	}

	s := &service.Service{
		InsecureSkipVerify: svc.InsecureSkipVerify,
		PassHostHeader:     svc.PassHostHeader,
		H2C:                svc.H2C,
		Retry:              defaultRetry,
		LoadBalancer: service.LoadBalancer{
			Servers:            make([]service.Server, 0, len(t.g.Targets)),
			Strategy:           svc.Strategy,
			Sticky:             svc.Sticky,
			HealthCheck:        svc.HealthCheck,
			PassiveHealthCheck: svc.PassiveHealthCheck,
		},
	}

	if svc.Retry != nil {
		s.Retry = *svc.Retry
	}

	if svc.ServerName != "" {
		s.TLS = &service.UpstreamTLS{ServerName: svc.ServerName}
	}

	for _, ip := range t.g.Targets {
		s.LoadBalancer.Servers = append(s.LoadBalancer.Servers, service.Server{
			URL: scheme + "://" + net.JoinHostPort(ip, strconv.Itoa(port)),
		})
	}

	t.out.server.HTTP.Middlewares[mwName] = shttp.HTTPMiddleware{ServiceMiddleware: s}

	return mwName, nil
}

func (t *translator) httpRouters() {
	for _, name := range keys(t.lc.HTTP.Routers) {
		if err := t.httpRouter(name, t.lc.HTTP.Routers[name]); err != nil {
			t.warn("http router %s: %w", name, err)
		}
	}
}

func (t *translator) httpRouter(name string, r httpRouter) error {
	entrypoints := r.EntryPoints
	switch {
	case len(entrypoints) == 0:
		entrypoints = t.claims.defaultEntrypoints(r.TLS, t.cfg.TCP.EntryPoints)
		if len(t.cfg.HTTP.EntryPoints) > 0 {
			entrypoints = slices.DeleteFunc(entrypoints, func(e string) bool { return !slices.Contains(t.cfg.HTTP.EntryPoints, e) })
		}

		if len(entrypoints) == 0 {
			return errors.New("no entrypoint is available, set entrypoints")
		}
	case len(t.cfg.HTTP.EntryPoints) > 0:
		if bad := notIn(entrypoints, t.cfg.HTTP.EntryPoints); bad != "" {
			return fmt.Errorf("entrypoint %s is not allowed, see providers.docker.http.entrypoints", bad)
		}
	}

	middlewares, err := t.resolve("http", r.Middlewares, t.lc.HTTP.Middlewares)
	if err != nil {
		return err
	}

	svcName, err := serviceName(r.Service, t.lc.HTTP.Services)
	if err != nil {
		return err
	}

	svc, err := t.httpService(svcName)
	if err != nil {
		return err
	}

	middlewares = append(middlewares, svc)

	paths := r.Path
	if len(paths) == 0 {
		paths = []string{"/*"}
	}

	hosts := r.Host
	if len(hosts) == 0 {
		hosts = []string{""}
	}

	var errs []error

	for i, host := range hosts {
		if err := t.claims.claimHTTP(host, paths, entrypoints, r.TLS); err != nil {
			errs = append(errs, err)

			continue
		}

		router := shttp.Router{
			Host:        host,
			Path:        slices.Clone(paths),
			Middlewares: slices.Clone(middlewares),
			EntryPoints: slices.Clone(entrypoints),
		}

		if r.TLS {
			router.TLS = &struct{}{}
		}

		routerName := t.name("http", name)
		if len(hosts) > 1 {
			routerName += "#" + strconv.Itoa(i)
		}

		t.out.server.HTTP.Routers[routerName] = router
	}

	return errors.Join(errs...)
}

// streamEntrypoints checks the entrypoints of a tcp or udp router: they must
// be listed in the provider config, a stream router owns its entrypoint.
func streamEntrypoints(kind string, entrypoints, allowed []string) error {
	if len(entrypoints) == 0 {
		return errors.New("entrypoints are required")
	}

	if bad := notIn(entrypoints, allowed); bad != "" {
		return fmt.Errorf("entrypoint %s is not allowed, see providers.docker.%s.entrypoints", bad, kind)
	}

	return nil
}

func (t *translator) streamRouters(kind string, routers map[string]streamRouter, local map[string]any, allowed []string, build func(service string) (string, error), add func(name string, r streamRouter, middlewares []string)) {
	for _, name := range keys(routers) {
		r := routers[name]

		err := func() error {
			if err := streamEntrypoints(kind, r.EntryPoints, allowed); err != nil {
				return err
			}

			middlewares, err := t.resolve(kind, r.Middlewares, local)
			if err != nil {
				return err
			}

			svc, err := build(r.Service)
			if err != nil {
				return err
			}

			routerName := t.name(kind, name)
			if err := t.claims.claimStream(kind, r.EntryPoints, routerName); err != nil {
				return err
			}

			add(routerName, r, append(middlewares, svc))

			return nil
		}()
		if err != nil {
			t.warn("%s router %s: %w", kind, name, err)
		}
	}
}

func (t *translator) tcpRouters() {
	t.streamRouters("tcp", t.lc.TCP.Routers, t.lc.TCP.Middlewares, t.cfg.TCP.EntryPoints,
		func(service string) (string, error) {
			svcName, err := serviceName(service, t.lc.TCP.Services)
			if err != nil {
				return "", err
			}

			return t.tcpService(svcName)
		},
		func(name string, r streamRouter, middlewares []string) {
			t.out.server.TCP.Routers[name] = tcp.Router{EntryPoints: slices.Clone(r.EntryPoints), Middlewares: middlewares}
		},
	)
}

func (t *translator) tcpService(name string) (string, error) {
	svc := t.lc.TCP.Services[name]

	label := name
	if label == "" {
		label = "default"
	}

	mwName := t.name("tcp-service", label)
	if _, ok := t.out.server.TCP.Middlewares[mwName]; ok {
		return mwName, nil
	}

	port, err := t.port("tcp", label, svc.Port)
	if err != nil {
		return "", err
	}

	if len(t.g.Targets) == 0 {
		// a load balancer needs servers, refuse connections instead
		t.out.server.TCP.Middlewares[mwName] = tcp.TCPMiddleware{BlockMiddleware: &tcpblock.Block{}}

		return mwName, nil
	}

	lb := &tcplb.LoadBalancer{
		Strategy:             svc.Strategy,
		Retry:                svc.Retry,
		DialTimeout:          svc.DialTimeout,
		DisableNagle:         svc.DisableNagle,
		ProxyProtocol:        svc.ProxyProtocol,
		ProxyProtocolVersion: svc.ProxyProtocolVersion,
		HealthCheck:          svc.HealthCheck,
		PassiveHealthCheck:   svc.PassiveHealthCheck,
	}

	for _, ip := range t.g.Targets {
		lb.Servers = append(lb.Servers, netlb.Server{Address: net.JoinHostPort(ip, strconv.Itoa(port))})
	}

	t.out.server.TCP.Middlewares[mwName] = tcp.TCPMiddleware{LoadBalancerMiddleware: lb}

	return mwName, nil
}

func (t *translator) udpRouters() {
	t.streamRouters("udp", t.lc.UDP.Routers, t.lc.UDP.Middlewares, t.cfg.UDP.EntryPoints,
		func(service string) (string, error) {
			svcName, err := serviceName(service, t.lc.UDP.Services)
			if err != nil {
				return "", err
			}

			return t.udpService(svcName)
		},
		func(name string, r streamRouter, middlewares []string) {
			t.out.server.UDP.Routers[name] = udp.Router{EntryPoints: slices.Clone(r.EntryPoints), Middlewares: middlewares}
		},
	)
}

func (t *translator) udpService(name string) (string, error) {
	svc := t.lc.UDP.Services[name]

	label := name
	if label == "" {
		label = "default"
	}

	mwName := t.name("udp-service", label)
	if _, ok := t.out.server.UDP.Middlewares[mwName]; ok {
		return mwName, nil
	}

	port, err := t.port("udp", label, svc.Port)
	if err != nil {
		return "", err
	}

	if len(t.g.Targets) == 0 {
		t.out.server.UDP.Middlewares[mwName] = udp.UDPMiddleware{BlockMiddleware: &udpblock.Block{}}

		return mwName, nil
	}

	lb := &udplb.LoadBalancer{
		Strategy:           svc.Strategy,
		SessionTimeout:     svc.SessionTimeout,
		MaxSessions:        svc.MaxSessions,
		HealthCheck:        svc.HealthCheck,
		PassiveHealthCheck: svc.PassiveHealthCheck,
	}

	for _, ip := range t.g.Targets {
		lb.Servers = append(lb.Servers, netlb.Server{Address: net.JoinHostPort(ip, strconv.Itoa(port))})
	}

	t.out.server.UDP.Middlewares[mwName] = udp.UDPMiddleware{LoadBalancerMiddleware: lb}

	return mwName, nil
}

func notIn(list, allowed []string) string {
	for _, v := range list {
		if !slices.Contains(allowed, v) {
			return v
		}
	}

	return ""
}

// translateAll builds the fragments of all groups. Older groups claim routes
// first; routes of other layers are never taken over.
func translateAll(cfg *Config, groups []*group, others *server.Server) (map[string]*fragment, map[string]error) {
	slices.SortFunc(groups, func(a, b *group) int {
		if c := a.Created.Compare(b.Created); c != 0 {
			return c
		}

		return strings.Compare(a.Name, b.Name)
	})

	cl := newClaims(others)
	out := make(map[string]*fragment, len(groups))
	errs := make(map[string]error)

	for _, g := range groups {
		f, err := translate(cfg, g, cl)
		if err != nil {
			errs[g.Name] = err

			continue
		}

		out[g.Name] = f
	}

	return out, errs
}

// withoutOwned returns the configuration of the other layers.
func withoutOwned(s *server.Server) *server.Server {
	out := newServer()
	if s == nil {
		return out
	}

	out.EntryPoints = s.EntryPoints

	copyNotOwned(out.HTTP.Routers, s.HTTP.Routers)
	copyNotOwned(out.HTTP.Middlewares, s.HTTP.Middlewares)
	copyNotOwned(out.TCP.Routers, s.TCP.Routers)
	copyNotOwned(out.TCP.Middlewares, s.TCP.Middlewares)
	copyNotOwned(out.UDP.Routers, s.UDP.Routers)
	copyNotOwned(out.UDP.Middlewares, s.UDP.Middlewares)

	return out
}

func copyNotOwned[V any](dst, src map[string]V) {
	for k, v := range src {
		if !isOwned(k) {
			dst[k] = v
		}
	}
}

// merge combines fragments into one layer.
func merge(fragments map[string]*fragment) *server.Server {
	out := newServer()

	for _, f := range fragments {
		for k, v := range f.server.HTTP.Routers {
			out.HTTP.Routers[k] = v
		}
		for k, v := range f.server.HTTP.Middlewares {
			out.HTTP.Middlewares[k] = v
		}
		for k, v := range f.server.TCP.Routers {
			out.TCP.Routers[k] = v
		}
		for k, v := range f.server.TCP.Middlewares {
			out.TCP.Middlewares[k] = v
		}
		for k, v := range f.server.UDP.Routers {
			out.UDP.Routers[k] = v
		}
		for k, v := range f.server.UDP.Middlewares {
			out.UDP.Middlewares[k] = v
		}
	}

	return out
}
