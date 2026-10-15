package kubernetes

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
)

// Requests are handled only by the routes of the most specific listener
// matching the host, as in the GatewayHTTPListenerIsolation conformance test.
func TestHTTPListenerIsolation(t *testing.T) {
	listeners := []gwv1.Listener{
		httpListener("empty-hostname", 80, ""),
		httpListener("wildcard-example-com", 80, "*.example.com"),
		httpListener("wildcard-foo-example-com", 80, "*.foo.example.com"),
		httpListener("abc-foo-example-com", 80, "abc.foo.example.com"),
	}

	allHosts := []string{"bar.com", "*.example.com", "*.foo.example.com", "abc.foo.example.com"}

	for _, tt := range []struct {
		name  string
		hosts []string
	}{
		{"hostnames only on listeners", nil},
		{"intersecting hostnames", allHosts},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, slice := serviceObj("app", "web", 80)

			var routes []*gwv1.HTTPRoute
			for _, l := range listeners {
				r := routeObj("app", string(l.Name), "gw", tt.hosts, backendRule("/"+string(l.Name), "web", 80))
				r.Spec.ParentRefs[0].SectionName = ptr(l.Name)
				routes = append(routes, r)
			}

			res := Translate(&Config{}, &Snapshot{
				GatewayClasses: []*gwv1.GatewayClass{class()},
				Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", listeners...)},
				HTTPRoutes:     routes,
				Services:       []*corev1.Service{svc},
				EndpointSlices: []*discoveryv1.EndpointSlice{slice},
			})

			for _, c := range []struct {
				host, listener string
			}{
				{"bar.com", "empty-hostname"},
				{"bar.example.com", "wildcard-example-com"},
				{"bar.foo.example.com", "wildcard-foo-example-com"},
				{"abc.foo.example.com", "abc-foo-example-com"},
			} {
				for _, l := range listeners {
					path := "/" + string(l.Name)
					got := routedTo(t, res.Server.HTTP, c.host, path)
					if want := string(l.Name) == c.listener; got != want {
						t.Errorf("%s%s routed = %v, want %v", c.host, path, got, want)
					}
				}
			}
		})
	}
}

// routedTo reports whether a request for host and path is served by a rule
// with that path prefix, following the router host selection: exact host,
// longest wildcard, then the host-less router.
func routedTo(t *testing.T, cfg shttp.HTTP, host, path string) bool {
	t.Helper()

	byHost := map[string]shttp.Router{}
	for _, r := range cfg.Routers {
		byHost[r.Host] = r
	}

	candidates := []string{host}
	for i := strings.IndexByte(host, '.'); i > 0; {
		candidates = append(candidates, "*"+host[i:])

		next := strings.IndexByte(host[i+1:], '.')
		if next < 0 {
			break
		}

		i += next + 1
	}

	candidates = append(candidates, "")

	for _, c := range candidates {
		router, ok := byHost[c]
		if !ok {
			continue
		}

		g := cfg.Middlewares[router.Middlewares[0]].Gateway
		if g == nil {
			t.Fatalf("router %q has no gateway", c)
		}

		for _, r := range g.Rules {
			for _, m := range r.Matches {
				if m.Path != nil && m.Path.Value == path {
					return true
				}
			}
		}

		return false
	}

	return false
}
