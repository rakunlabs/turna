package kubernetes

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestHTTPGRPCHostnameIntersectionConflict(t *testing.T) {
	for _, tt := range []struct {
		name       string
		http, grpc []string
		conflict   bool
	}{
		{name: "wildcard-exact", http: []string{"*.example.com"}, grpc: []string{"api.example.com"}, conflict: true},
		{name: "exact-wildcard", http: []string{"api.example.com"}, grpc: []string{"*.example.com"}, conflict: true},
		{name: "catch-all", grpc: []string{"api.example.com"}, conflict: true},
		{name: "nested-wildcards", http: []string{"*.example.com"}, grpc: []string{"*.sub.example.com"}, conflict: true},
		{name: "one-overlapping-host", http: []string{"*.example.com"}, grpc: []string{"api.other.com", "api.example.com"}, conflict: true},
		{name: "wildcard-excludes-apex", http: []string{"*.example.com"}, grpc: []string{"example.com"}},
		{name: "disjoint", http: []string{"api.example.com"}, grpc: []string{"grpc.example.com"}},
	} {
		for _, olderGRPC := range []bool{false, true} {
			t.Run(tt.name+map[bool]string{false: "/http-older", true: "/grpc-older"}[olderGRPC], func(t *testing.T) {
				httpRoute := routeObj("app", "http", "gw", tt.http, gwv1.HTTPRouteRule{})
				grpcRoute := &gwv1.GRPCRoute{ObjectMeta: objMeta("app", "grpc"), Spec: gwv1.GRPCRouteSpec{
					CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: "gw"}}},
					Rules:           []gwv1.GRPCRouteRule{{}},
				}}
				for _, h := range tt.grpc {
					grpcRoute.Spec.Hostnames = append(grpcRoute.Spec.Hostnames, gwv1.Hostname(h))
				}
				old, next := metav1.NewTime(time.Unix(1, 0)), metav1.NewTime(time.Unix(2, 0))
				httpRoute.CreationTimestamp, grpcRoute.CreationTimestamp = old, next
				if olderGRPC {
					httpRoute.CreationTimestamp, grpcRoute.CreationTimestamp = next, old
				}
				res := Translate(&Config{}, &Snapshot{
					GatewayClasses: []*gwv1.GatewayClass{class()},
					Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
					HTTPRoutes:     []*gwv1.HTTPRoute{httpRoute}, GRPCRoutes: []*gwv1.GRPCRoute{grpcRoute},
				})
				for _, route := range []routeKey{{Kind: kindRoute, nsName: key(httpRoute)}, {Kind: kindGRPCRoute, nsName: key(grpcRoute)}} {
					accepted := condition(t, res.Routes[route][0].Conditions, string(gwv1.RouteConditionAccepted))
					want := metav1.ConditionTrue
					if tt.conflict && (route.Kind == kindRoute) == olderGRPC {
						want = metav1.ConditionFalse
					}
					if accepted.Status != want {
						t.Errorf("%s Accepted = %s, want %s", route.Kind, accepted.Status, want)
					}
				}
				wantAttached := int32(2)
				if tt.conflict {
					wantAttached = 1
				}
				if got := res.Gateways[nsName{"app", "gw"}].Listeners[0].AttachedRoutes; got != wantAttached {
					t.Errorf("attached routes = %d, want %d", got, wantAttached)
				}
			})
		}
	}
}
