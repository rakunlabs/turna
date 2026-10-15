package kubernetes

import (
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/gateway"
)

// Rule fields that are not implemented must show up in the route status
// instead of being ignored, while the rule keeps routing.
func TestUnsupportedRuleFieldsReported(t *testing.T) {
	svc, slice := serviceObj("app", "web", 80)

	withSession := backendRule("/", "web", 80)
	withSession.SessionPersistence = &gwv1.SessionPersistence{}

	grpcRoute := &gwv1.GRPCRoute{ObjectMeta: objMeta("app", "g"), Spec: gwv1.GRPCRouteSpec{
		CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: "gw"}}},
		Hostnames:       []gwv1.Hostname{"grpc.example.com"},
		Rules:           []gwv1.GRPCRouteRule{{SessionPersistence: &gwv1.SessionPersistence{}}},
	}}

	snap := &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes: []*gwv1.HTTPRoute{
			routeObj("app", "session", "gw", []string{"session.example.com"}, withSession),
		},
		GRPCRoutes:     []*gwv1.GRPCRoute{grpcRoute},
		Services:       []*corev1.Service{svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{slice},
	}

	res := Translate(&Config{}, snap)

	for _, tt := range []struct {
		route routeKey
		msg   string
	}{
		{routeKey{Kind: kindRoute, nsName: nsName{"app", "session"}}, "sessionPersistence is not supported"},
		{routeKey{Kind: kindGRPCRoute, nsName: nsName{"app", "g"}}, "sessionPersistence is not supported"},
	} {
		parents := res.Routes[tt.route]
		if len(parents) != 1 {
			t.Fatalf("%v parents = %v", tt.route, parents)
		}

		if c := condition(t, parents[0].Conditions, string(gwv1.RouteConditionAccepted)); c.Status != metav1.ConditionTrue {
			t.Errorf("%v accepted = %+v", tt.route, c)
		}

		c := condition(t, parents[0].Conditions, string(gwv1.RouteConditionResolvedRefs))
		if c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.RouteReasonUnsupportedValue) || c.Message != tt.msg {
			t.Errorf("%v resolved refs = %+v", tt.route, c)
		}
	}

	// the HTTP rules still route to the backend
	var upstreams int
	for _, m := range res.Server.HTTP.Middlewares {
		if m.ServiceMiddleware != nil {
			upstreams += len(m.ServiceMiddleware.LoadBalancer.Servers)
		}
	}

	if upstreams == 0 {
		t.Fatal("rules with unsupported fields stopped routing")
	}
}

func TestRouteRetry(t *testing.T) {
	svc, slice := serviceObj("app", "web", 80)

	rule := backendRule("/", "web", 80)
	rule.Retry = &gwv1.HTTPRouteRetry{Codes: []gwv1.HTTPRouteRetryStatusCode{500, 503}, Attempts: ptr(3), Backoff: ptr(gwv1.Duration("100ms"))}
	rule.Timeouts = &gwv1.HTTPRouteTimeouts{Request: ptr(gwv1.Duration("10s")), BackendRequest: ptr(gwv1.Duration("2s"))}

	plain := backendRule("/", "web", 80)
	plain.Retry = &gwv1.HTTPRouteRetry{}

	disabled := backendRule("/", "web", 80)
	disabled.Retry = &gwv1.HTTPRouteRetry{Attempts: ptr(0)}

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes: []*gwv1.HTTPRoute{
			routeObj("app", "r", "gw", []string{"a.example.com"}, rule),
			routeObj("app", "p", "gw", []string{"b.example.com"}, plain),
			routeObj("app", "d", "gw", []string{"c.example.com"}, disabled),
			routeObj("app", "n", "gw", []string{"d.example.com"}, backendRule("/", "web", 80)),
		},
		Services:       []*corev1.Service{svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{slice},
	})

	for _, name := range []string{"r", "p", "d", "n"} {
		c := condition(t, res.Routes[routeKey{Kind: kindRoute, nsName: nsName{"app", name}}][0].Conditions, string(gwv1.RouteConditionResolvedRefs))
		if c.Status != metav1.ConditionTrue {
			t.Fatalf("%s resolved refs = %+v", name, c)
		}
	}

	var got []gateway.Rule
	for _, m := range res.Server.HTTP.Middlewares {
		if m.Gateway != nil && !m.Gateway.Passthrough {
			got = append(got, m.Gateway.Rules...)
		}
	}

	if len(got) != 4 {
		t.Fatalf("rules = %+v", got)
	}

	seen := map[int]bool{}

	for _, r := range got {
		// rules with a route retry must not retry again in the service
		svc := res.Server.HTTP.Middlewares[r.Backends[0].Middlewares[len(r.Backends[0].Middlewares)-1]].ServiceMiddleware
		if svc == nil {
			t.Fatalf("rule backend is not a service: %+v", r.Backends[0])
		}

		if r.Retry == nil {
			if svc.Retry != 2 {
				t.Fatalf("service retry without route retry = %d", svc.Retry)
			}

			continue
		}

		if svc.Retry != 0 {
			t.Fatalf("service retry with route retry = %d", svc.Retry)
		}

		if r.Retry.Attempts == nil {
			t.Fatalf("attempts not set: %+v", r.Retry)
		}

		seen[*r.Retry.Attempts] = true

		switch *r.Retry.Attempts {
		case 3:
			if !slices.Equal(r.Retry.Codes, []int{500, 503}) || r.Retry.Backoff != 100*time.Millisecond || r.Timeout != 10*time.Second || r.BackendTimeout != 2*time.Second {
				t.Fatalf("retry rule = %+v retry = %+v", r, r.Retry)
			}
		case 1, 0:
			if len(r.Retry.Codes) != 0 || r.Retry.Backoff != 0 {
				t.Fatalf("default retry = %+v", r.Retry)
			}
		default:
			t.Fatalf("attempts = %d", *r.Retry.Attempts)
		}
	}

	if !seen[0] || !seen[1] || !seen[3] {
		t.Fatalf("attempts seen = %v", seen)
	}
}
