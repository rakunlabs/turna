package kubernetes

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server/netlb"
	"github.com/rakunlabs/turna/pkg/server/tcp"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/block"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/loadbalancer"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/snirouter"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/tlsterminate"
	"github.com/rakunlabs/turna/pkg/server/udp"
	udpblock "github.com/rakunlabs/turna/pkg/server/udp/middleware/block"
	udplb "github.com/rakunlabs/turna/pkg/server/udp/middleware/loadbalancer"
)

// streamBackend is a weighted backend of a TLS, TCP or UDP route. An invalid
// backend has no addresses and rejects its share of connections.
type streamBackend struct {
	weight int
	addrs  []string
}

// streamBackends resolves the backends of a stream route.
func (t *translator) streamBackends(kind gwv1.Kind, namespace string, refs []gwv1.BackendRef, protocol corev1.Protocol) ([]streamBackend, *refError) {
	var firstErr *refError

	out := make([]streamBackend, 0, len(refs))

	for _, ref := range refs {
		b := streamBackend{weight: int(ptrOr(ref.Weight, 1))}

		addrs, _, err := t.resolveAddresses(kind, namespace, ref.BackendObjectReference, protocol)
		if err != nil && firstErr == nil {
			firstErr = err
		}

		b.addrs = addrs
		out = append(out, b)
	}

	return out, firstErr
}

// servers spreads backend weights over their endpoints.
func streamServers(backends []streamBackend) []netlb.Server {
	var servers []netlb.Server

	for _, b := range backends {
		if b.weight <= 0 || len(b.addrs) == 0 {
			continue
		}

		for _, a := range b.addrs {
			// weight per endpoint keeps the backend share independent of its
			// endpoint count
			servers = append(servers, netlb.Server{Address: a, Weight: max(1, b.weight*100/len(b.addrs))})
		}
	}

	return servers
}

// invalidShare reports the share of connections that must be rejected
// because of invalid backends, as weight of valid and invalid backends.
func invalidShare(backends []streamBackend) (valid, invalid int) {
	for _, b := range backends {
		if b.weight <= 0 {
			continue
		}

		if len(b.addrs) == 0 {
			invalid += b.weight
		} else {
			valid += b.weight
		}
	}

	return valid, invalid
}

// streamTarget returns the middleware names forwarding to the backends, an
// empty result rejects connections.
func (t *translator) streamTarget(entry string, idx int, backends []streamBackend, udpRoute bool) []string {
	servers := streamServers(backends)
	if len(servers) == 0 {
		return nil
	}

	slices.SortFunc(servers, func(a, b netlb.Server) int { return cmp.Compare(a.Address, b.Address) })

	name := sanitize("k8s", entry, "lb", strconv.Itoa(idx))
	srv := t.out.Server

	if udpRoute {
		srv.UDP.Middlewares[name] = udp.UDPMiddleware{LoadBalancerMiddleware: &udplb.LoadBalancer{
			Servers:            servers,
			Strategy:           "random",
			PassiveHealthCheck: &netlb.PassiveHealthCheck{MaxFails: 3},
		}}
		valid, invalid := invalidShare(backends)
		if invalid > 0 {
			t.out.Server.UDP.Middlewares[name+"-share"] = udp.UDPMiddleware{BlockMiddleware: &udpblock.Block{Percent: float64(invalid) * 100 / float64(valid+invalid)}}
			return []string{name + "-share", name}
		}

		return []string{name}
	}

	srv.TCP.Middlewares[name] = tcp.TCPMiddleware{LoadBalancerMiddleware: &loadbalancer.LoadBalancer{
		Servers:            servers,
		Strategy:           "random",
		Retry:              ptrOr(t.cfg.Backend.Retry, 2),
		PassiveHealthCheck: &netlb.PassiveHealthCheck{MaxFails: 3},
	}}

	valid, invalid := invalidShare(backends)
	if invalid > 0 {
		// invalid backends reject their share of connections
		srv.TCP.Middlewares[name+"-share"] = tcp.TCPMiddleware{BlockMiddleware: &block.Block{Percent: float64(invalid) * 100 / float64(valid+invalid)}}

		return []string{name + "-share", name}
	}

	return []string{name}
}

// buildStreams creates TCP and UDP routers for stream entrypoints.
func (t *translator) buildStreams() {
	for entry, fam := range t.families {
		switch fam {
		case familyTCP:
			t.buildTCP(entry)
		case familyUDP:
			t.buildUDP(entry)
		case familyTLS:
			t.buildTLS(entry)
		}
	}
}

// rejectAll returns the name of a middleware closing every connection.
func (t *translator) rejectAll(entry string) string {
	name := sanitize("k8s", entry, "reject")
	t.out.Server.TCP.Middlewares[name] = tcp.TCPMiddleware{BlockMiddleware: &block.Block{Percent: 100}}

	return name
}

// buildTCP routes a TCP entrypoint to the backends of the oldest route, a
// TCP listener without hostnames cannot distinguish routes.
func (t *translator) buildTCP(entry string) {
	routes := t.streams[entry]

	var names []string
	if len(routes) > 0 {
		names = t.streamTarget(entry, 0, routes[0].backends, false)
	}

	if len(names) == 0 {
		names = []string{t.rejectAll(entry)}
	}

	t.out.Server.TCP.Routers[sanitize("k8s", entry)] = tcp.Router{EntryPoints: []string{entry}, Middlewares: names}
}

func (t *translator) buildUDP(entry string) {
	routes := t.streams[entry]

	var names []string
	if len(routes) > 0 {
		names = t.streamTarget(entry, 0, routes[0].backends, true)
	}

	if len(names) == 0 {
		// no backend: drop datagrams
		name := sanitize("k8s", entry, "drop")
		t.out.Server.UDP.Middlewares[name] = udp.UDPMiddleware{BlockMiddleware: &udpblock.Block{Percent: 100}}
		names = []string{name}
	}

	t.out.Server.UDP.Routers[sanitize("k8s", entry)] = udp.Router{EntryPoints: []string{entry}, Middlewares: names}
}

// buildTLS routes a TLS entrypoint by SNI. Passthrough listeners forward the
// encrypted stream, terminate listeners decrypt it with the listener
// certificates first.
func (t *translator) buildTLS(entry string) {
	type sniRoute struct {
		host  string
		names []string
	}

	var routes []sniRoute

	for i, r := range t.streams[entry] {
		target := t.streamTarget(entry, i, r.backends, false)
		if len(target) == 0 {
			target = []string{t.rejectAll(entry)}
		}

		if !r.li.passthrough {
			name := sanitize("k8s", entry, "terminate", r.li.gw.Namespace, r.li.gw.Name, string(r.li.spec.Name))
			term := &tlsterminate.TLSTerminate{}

			for _, c := range r.li.certs {
				term.Certificates = append(term.Certificates, tlsterminate.Certificate{Cert: c.Cert, Key: c.Key})
			}

			t.out.Server.TCP.Middlewares[name] = tcp.TCPMiddleware{TLSTerminateMiddleware: term}
			target = append([]string{name}, target...)
		}

		for _, h := range r.hosts {
			if h == "" {
				h = "*"
			}

			// the oldest route wins a hostname
			if slices.ContainsFunc(routes, func(x sniRoute) bool { return x.host == h }) {
				continue
			}

			routes = append(routes, sniRoute{host: h, names: target})
		}
	}

	// exact names first, then longer wildcards
	slices.SortStableFunc(routes, func(a, b sniRoute) int {
		aw, bw := strings.HasPrefix(a.host, "*"), strings.HasPrefix(b.host, "*")
		if aw != bw {
			if aw {
				return 1
			}

			return -1
		}

		return cmp.Compare(len(b.host), len(a.host))
	})

	router := &snirouter.SNIRouter{}
	for _, r := range routes {
		router.Routes = append(router.Routes, snirouter.Route{SNI: []string{r.host}, Middlewares: r.names})
	}

	name := sanitize("k8s", entry, "sni")

	if len(router.Routes) == 0 {
		t.out.Server.TCP.Routers[sanitize("k8s", entry)] = tcp.Router{EntryPoints: []string{entry}, Middlewares: []string{t.rejectAll(entry)}}

		return
	}

	t.out.Server.TCP.Middlewares[name] = tcp.TCPMiddleware{SNIRouterMiddleware: router}
	t.out.Server.TCP.Routers[sanitize("k8s", entry)] = tcp.Router{EntryPoints: []string{entry}, Middlewares: []string{name}}
}
