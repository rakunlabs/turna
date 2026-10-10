package kubernetes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
)

func TestHTTPBackendHonorsH2CAppProtocol(t *testing.T) {
	for _, app := range []string{"kubernetes.io/h2c", "kubernetes.io/ws", "https", ""} {
		t.Run(app, func(t *testing.T) {
			svc, slice := serviceObj("demo", "web", 80)
			svc.Spec.Ports[0].AppProtocol = ptr(app)
			tr := &translator{cfg: &Config{}, idx: newIndex(&Snapshot{Services: []*corev1.Service{svc}, EndpointSlices: []*discoveryv1.EndpointSlice{slice}}), middlewares: map[string]shttp.HTTPMiddleware{}}
			r := routeObj("demo", "r", "gw", nil, backendRule("/", "web", 80))
			b, err := tr.backend(r, 0, 0, r.Spec.Rules[0].BackendRefs[0])
			if err != nil {
				t.Fatal(err)
			}
			upstream := tr.middlewares[b.Middlewares[0]].ServiceMiddleware
			if upstream.H2C != (app == "kubernetes.io/h2c") {
				t.Errorf("H2C = %v for appProtocol %q", upstream.H2C, app)
			}
		})
	}
}

func TestGRPCMirrorGroupsEndpointsAndHonorsSampling(t *testing.T) {
	for _, sample := range []string{"default", "percent", "fraction"} {
		t.Run(sample, func(t *testing.T) {
			svc, slice := serviceObj("demo", "shadow", 80)
			slice.Endpoints = []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}, {Addresses: []string{"10.0.0.2"}}}
			svc.Spec.Ports[0].AppProtocol = ptr("https")
			tr := &translator{cfg: &Config{}, idx: newIndex(&Snapshot{Services: []*corev1.Service{svc}, EndpointSlices: []*discoveryv1.EndpointSlice{slice}})}
			filter := &gwv1.HTTPRequestMirrorFilter{BackendRef: gwv1.BackendObjectReference{Name: "shadow", Port: ptr(gwv1.PortNumber(80))}}
			want := 100.0
			switch sample {
			case "percent":
				filter.Percent = ptr(int32(25))
				want = 25
			case "fraction":
				filter.Fraction = &gwv1.Fraction{Numerator: 1, Denominator: ptr(int32(8))}
				want = 12.5
			}
			filters, err := tr.grpcFilters(&gwv1.GRPCRoute{ObjectMeta: metav1.ObjectMeta{Namespace: "demo"}}, []gwv1.GRPCRouteFilter{{Type: gwv1.GRPCRouteFilterRequestMirror, RequestMirror: filter}})
			if err != nil {
				t.Fatal(err)
			}
			m := filters[0].Mirror
			if len(m.Servers) != 1 {
				t.Fatalf("mirror targets = %d, want one target with alternative endpoints", len(m.Servers))
			}
			if len(m.Servers[0].Endpoints) != 2 || m.Servers[0].Endpoints[0] != "https://10.0.0.1:8080" || *m.Servers[0].Percent != want {
				t.Fatalf("mirror target = %+v, want HTTPS endpoints and %v%% sampling", m.Servers[0], want)
			}
		})
	}
}

func TestUDPInvalidBackendShare(t *testing.T) {
	for _, invalid := range []int{0, 1, 3} {
		tr := &translator{out: &Result{Server: Translate(&Config{}, &Snapshot{}).Server}}
		backends := []streamBackend{{weight: 1, addrs: []string{"127.0.0.1:8080"}}, {weight: invalid}}
		names := tr.streamTarget("k8s-udp-53", 0, backends, true)
		if invalid == 0 {
			if len(names) != 1 {
				t.Fatal("zero-weight invalid backend added a drop middleware")
			}
			continue
		}
		if len(names) != 2 {
			t.Fatalf("middleware chain = %v, want drop-share then load balancer", names)
		}
		block := tr.out.Server.UDP.Middlewares[names[0]].BlockMiddleware
		want := float64(invalid) * 100 / float64(1+invalid)
		if block == nil || block.Percent != want {
			t.Fatalf("drop share = %+v, want %v%%", block, want)
		}
	}
}

func TestHTTPRouteH2CUpstream(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}))
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	upstream.Config.Protocols = &protocols
	upstream.Start()
	t.Cleanup(upstream.Close)
	svcs, slices := backendObjects(backend{name: "h2c", addr: upstream.Listener.Addr().String(), port: 80})
	svcs[0].Spec.Ports[0].AppProtocol = ptr("kubernetes.io/h2c")
	local := freePort(t)
	applyTranslate(t, &Config{Address: "127.0.0.1", PortOffset: local - 80}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes:     []*gwv1.HTTPRoute{routeObj("app", "r", "gw", nil, backendRule("/", "h2c", 80))},
		Services:       svcs, EndpointSlices: slices,
	})
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://127.0.0.1:" + strconv.Itoa(local) + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || string(body) != "HTTP/2.0" {
		t.Fatalf("upstream response = %d %q, err = %v", resp.StatusCode, body, err)
	}
}

func TestGRPCMirrorUsesGRPCReferenceGrant(t *testing.T) {
	svc, slice := serviceObj("backend", "shadow", 80)
	snap := &Snapshot{Services: []*corev1.Service{svc}, EndpointSlices: []*discoveryv1.EndpointSlice{slice}, ReferenceGrants: []*gwv1.ReferenceGrant{{
		ObjectMeta: objMeta("backend", "allow"),
		Spec: gwv1.ReferenceGrantSpec{
			From: []gwv1.ReferenceGrantFrom{{Group: groupGateway, Kind: kindGRPCRoute, Namespace: "app"}},
			To:   []gwv1.ReferenceGrantTo{{Group: "", Kind: kindService}},
		},
	}}}
	filter := &gwv1.HTTPRequestMirrorFilter{BackendRef: gwv1.BackendObjectReference{Namespace: ptr(gwv1.Namespace("backend")), Name: "shadow", Port: ptr(gwv1.PortNumber(80))}}
	tr := &translator{cfg: &Config{}, idx: newIndex(snap)}
	m, err := tr.requestMirror(kindGRPCRoute, "app", filter, true)
	if err != nil || m == nil || !m.H2C {
		t.Fatalf("GRPC mirror = %+v, err = %v", m, err)
	}
	if _, err := tr.requestMirror(kindRoute, "app", filter, false); err == nil || err.reason != string(gwv1.RouteReasonRefNotPermitted) {
		t.Fatalf("GRPC-only grant permitted HTTP mirror: %v", err)
	}
}
