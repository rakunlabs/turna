package kubernetes

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server/cert"
)

func objMeta(ns, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: ns, Name: name, Generation: 1}
}

func class() *gwv1.GatewayClass {
	return &gwv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "turna", Generation: 1},
		Spec:       gwv1.GatewayClassSpec{ControllerName: DefaultControllerName},
	}
}

func gatewayObj(ns, name string, listeners ...gwv1.Listener) *gwv1.Gateway {
	return &gwv1.Gateway{
		ObjectMeta: objMeta(ns, name),
		Spec:       gwv1.GatewaySpec{GatewayClassName: "turna", Listeners: listeners},
	}
}

func httpListener(name string, port int32, host string) gwv1.Listener {
	l := gwv1.Listener{Name: gwv1.SectionName(name), Port: gwv1.PortNumber(port), Protocol: gwv1.HTTPProtocolType}
	if host != "" {
		l.Hostname = ptr(gwv1.Hostname(host))
	}

	return l
}

func routeObj(ns, name, gw string, hosts []string, rules ...gwv1.HTTPRouteRule) *gwv1.HTTPRoute {
	r := &gwv1.HTTPRoute{
		ObjectMeta: objMeta(ns, name),
		Spec: gwv1.HTTPRouteSpec{
			CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{Name: gwv1.ObjectName(gw)}}},
			Rules:           rules,
		},
	}

	for _, h := range hosts {
		r.Spec.Hostnames = append(r.Spec.Hostnames, gwv1.Hostname(h))
	}

	return r
}

func backendRule(path, svc string, port int32) gwv1.HTTPRouteRule {
	return gwv1.HTTPRouteRule{
		Matches: []gwv1.HTTPRouteMatch{{Path: &gwv1.HTTPPathMatch{Type: ptr(gwv1.PathMatchPathPrefix), Value: ptr(path)}}},
		BackendRefs: []gwv1.HTTPBackendRef{{BackendRef: gwv1.BackendRef{
			BackendObjectReference: gwv1.BackendObjectReference{Name: gwv1.ObjectName(svc), Port: ptr(gwv1.PortNumber(port))},
		}}},
	}
}

func serviceObj(ns, name string, port int32) (*corev1.Service, *discoveryv1.EndpointSlice) {
	svc := &corev1.Service{
		ObjectMeta: objMeta(ns, name),
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.10",
			Ports:     []corev1.ServicePort{{Name: "http", Port: port}},
		},
	}

	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns, Name: name + "-abc",
			Labels: map[string]string{discoveryv1.LabelServiceName: name},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: ptr("http"), Port: ptr(int32(8080))}},
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr(true)}},
			{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr(false)}},
		},
	}

	return svc, slice
}

func condition(t *testing.T, conds []metav1.Condition, typ string) metav1.Condition {
	t.Helper()

	c := meta.FindStatusCondition(conds, typ)
	if c == nil {
		t.Fatalf("condition %s not found in %v", typ, conds)
	}

	return *c
}

func TestTranslateBasic(t *testing.T) {
	svc, slice := serviceObj("app", "web", 80)

	snap := &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes:     []*gwv1.HTTPRoute{routeObj("app", "r", "gw", []string{"example.com"}, backendRule("/", "web", 80))},
		Services:       []*corev1.Service{svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{slice},
	}

	res := Translate(&Config{PortOffset: 8000}, snap)

	ep, ok := res.Server.EntryPoints["k8s-80"]
	if !ok || ep.Address != ":8080" {
		t.Fatalf("entrypoints = %v", res.Server.EntryPoints)
	}

	router, ok := res.Server.HTTP.Routers[sanitize("k8s", "k8s-80", "example.com")]
	if !ok || router.Host != "example.com" || router.TLS != nil {
		t.Fatalf("routers = %v", res.Server.HTTP.Routers)
	}

	var upstreams []string
	for name, m := range res.Server.HTTP.Middlewares {
		if m.ServiceMiddleware != nil {
			for _, s := range m.ServiceMiddleware.LoadBalancer.Servers {
				upstreams = append(upstreams, name+"="+s.URL)
			}
		}
	}

	if len(upstreams) != 1 || !strings.HasSuffix(upstreams[0], "=http://10.0.0.1:8080") {
		t.Fatalf("upstreams = %v (not ready endpoints must be skipped)", upstreams)
	}

	gw := res.Gateways[nsName{"app", "gw"}]
	if c := condition(t, gw.Conditions, string(gwv1.GatewayConditionProgrammed)); c.Status != metav1.ConditionTrue {
		t.Fatalf("gateway programmed = %v", c)
	}

	if gw.Listeners[0].AttachedRoutes != 1 {
		t.Fatalf("attached routes = %d", gw.Listeners[0].AttachedRoutes)
	}

	parents := res.HTTPRouteStatus(nsName{"app", "r"})
	if len(parents) != 1 {
		t.Fatalf("route parents = %v", parents)
	}

	if c := condition(t, parents[0].Conditions, string(gwv1.RouteConditionAccepted)); c.Status != metav1.ConditionTrue {
		t.Fatalf("route accepted = %v", c)
	}

	if c := condition(t, parents[0].Conditions, string(gwv1.RouteConditionResolvedRefs)); c.Status != metav1.ConditionTrue {
		t.Fatalf("route resolved = %v", c)
	}

	if c := condition(t, res.GatewayClasses["turna"], string(gwv1.GatewayClassConditionStatusAccepted)); c.Status != metav1.ConditionTrue {
		t.Fatalf("class accepted = %v", c)
	}
}

func TestTranslateIgnoresOtherControllers(t *testing.T) {
	other := class()
	other.Name = "other"
	other.Spec.ControllerName = "example.com/other"

	gw := gatewayObj("app", "gw", httpListener("http", 80, ""))
	gw.Spec.GatewayClassName = "other"

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{other},
		Gateways:       []*gwv1.Gateway{gw},
	})

	if len(res.Server.EntryPoints) != 0 || len(res.Gateways) != 0 || len(res.GatewayClasses) != 0 {
		t.Fatalf("foreign resources translated: %+v", res)
	}
}

func TestTranslateCrossNamespace(t *testing.T) {
	svc, slice := serviceObj("backend", "api", 80)

	gw := gatewayObj("infra", "gw", httpListener("http", 80, ""))
	route := routeObj("app", "r", "gw", nil, backendRule("/", "api", 80))
	route.Spec.ParentRefs[0].Namespace = ptr(gwv1.Namespace("infra"))
	route.Spec.Rules[0].BackendRefs[0].Namespace = ptr(gwv1.Namespace("backend"))

	snap := &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		HTTPRoutes:     []*gwv1.HTTPRoute{route},
		Services:       []*corev1.Service{svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{slice},
	}

	// default allowedRoutes is Same namespace
	res := Translate(&Config{}, snap)
	c := condition(t, res.HTTPRouteStatus(key(route))[0].Conditions, string(gwv1.RouteConditionAccepted))
	if c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.RouteReasonNotAllowedByListeners) {
		t.Fatalf("accepted = %v", c)
	}

	gw.Spec.Listeners[0].AllowedRoutes = &gwv1.AllowedRoutes{Namespaces: &gwv1.RouteNamespaces{From: ptr(gwv1.NamespacesFromAll)}}

	res = Translate(&Config{}, snap)
	c = condition(t, res.HTTPRouteStatus(key(route))[0].Conditions, string(gwv1.RouteConditionResolvedRefs))
	if c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.RouteReasonRefNotPermitted) {
		t.Fatalf("resolved without grant = %v", c)
	}

	snap.ReferenceGrants = []*gwv1.ReferenceGrant{{
		ObjectMeta: objMeta("backend", "allow"),
		Spec: gwv1.ReferenceGrantSpec{
			From: []gwv1.ReferenceGrantFrom{{Group: gwv1.GroupName, Kind: "HTTPRoute", Namespace: "app"}},
			To:   []gwv1.ReferenceGrantTo{{Group: "", Kind: "Service"}},
		},
	}}

	res = Translate(&Config{}, snap)
	c = condition(t, res.HTTPRouteStatus(key(route))[0].Conditions, string(gwv1.RouteConditionResolvedRefs))
	if c.Status != metav1.ConditionTrue {
		t.Fatalf("resolved with grant = %v", c)
	}
}

// Namespaces that are not watched, as in namespace scoped mode, match no
// selector, not even an empty one.
func TestTranslateSelectorUnwatchedNamespace(t *testing.T) {
	gw := gatewayObj("infra", "gw", httpListener("http", 80, ""))
	gw.Spec.Listeners[0].AllowedRoutes = &gwv1.AllowedRoutes{Namespaces: &gwv1.RouteNamespaces{
		From:     ptr(gwv1.NamespacesFromSelector),
		Selector: &metav1.LabelSelector{},
	}}

	route := routeObj("app", "r", "gw", nil, backendRule("/", "api", 80))
	route.Spec.ParentRefs[0].Namespace = ptr(gwv1.Namespace("infra"))

	snap := &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		HTTPRoutes:     []*gwv1.HTTPRoute{route},
	}

	res := Translate(&Config{}, snap)
	c := condition(t, res.HTTPRouteStatus(key(route))[0].Conditions, string(gwv1.RouteConditionAccepted))
	if c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.RouteReasonNotAllowedByListeners) {
		t.Fatalf("accepted from unwatched namespace = %v", c)
	}

	snap.Namespaces = []*corev1.Namespace{{ObjectMeta: objMeta("", "app")}}

	res = Translate(&Config{}, snap)
	c = condition(t, res.HTTPRouteStatus(key(route))[0].Conditions, string(gwv1.RouteConditionAccepted))
	if c.Status != metav1.ConditionTrue {
		t.Fatalf("accepted from watched namespace = %v", c)
	}
}

func TestTranslateHostnames(t *testing.T) {
	svc, slice := serviceObj("app", "web", 80)
	gw := gatewayObj("app", "gw", httpListener("http", 80, "*.example.com"))

	cases := []struct {
		hosts    []string
		accepted bool
		routers  []string
	}{
		{nil, true, []string{"*.example.com"}},
		{[]string{"a.example.com", "a.other.com"}, true, []string{"a.example.com"}},
		{[]string{"*.b.example.com"}, true, []string{"*.b.example.com"}},
		{[]string{"other.com"}, false, nil},
	}

	for _, c := range cases {
		route := routeObj("app", "r", "gw", c.hosts, backendRule("/", "web", 80))

		res := Translate(&Config{}, &Snapshot{
			GatewayClasses: []*gwv1.GatewayClass{class()},
			Gateways:       []*gwv1.Gateway{gw},
			HTTPRoutes:     []*gwv1.HTTPRoute{route},
			Services:       []*corev1.Service{svc},
			EndpointSlices: []*discoveryv1.EndpointSlice{slice},
		})

		cond := condition(t, res.HTTPRouteStatus(key(route))[0].Conditions, string(gwv1.RouteConditionAccepted))
		if (cond.Status == metav1.ConditionTrue) != c.accepted {
			t.Errorf("%v: accepted = %v", c.hosts, cond)
		}

		var hosts []string
		for _, r := range res.Server.HTTP.Routers {
			if r.Host != "" {
				hosts = append(hosts, r.Host)
			}
		}

		if strings.Join(hosts, ",") != strings.Join(c.routers, ",") {
			t.Errorf("%v: router hosts = %v, want %v", c.hosts, hosts, c.routers)
		}
	}
}

func TestTranslateTLS(t *testing.T) {
	generated, err := cert.GenerateCertificate(cert.WithDNSNames("secure.example.com"))
	if err != nil {
		t.Fatal(err)
	}

	secret := &corev1.Secret{
		ObjectMeta: objMeta("app", "tls"),
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: generated.Certificate, corev1.TLSPrivateKeyKey: generated.PrivateKey},
	}

	l := gwv1.Listener{
		Name: "https", Port: 443, Protocol: gwv1.HTTPSProtocolType,
		Hostname: ptr(gwv1.Hostname("secure.example.com")),
		TLS:      &gwv1.ListenerTLSConfig{CertificateRefs: []gwv1.SecretObjectReference{{Name: "tls"}}},
	}

	missing := l
	missing.Name = "missing"
	missing.Hostname = ptr(gwv1.Hostname("missing.example.com"))
	missing.TLS = &gwv1.ListenerTLSConfig{CertificateRefs: []gwv1.SecretObjectReference{{Name: "nope"}}}

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", l, missing)},
		Secrets:        []*corev1.Secret{secret},
	})

	if len(res.Server.HTTP.TLS.Store) != 0 {
		t.Fatalf("shared tls store = %v", res.Server.HTTP.TLS.Store)
	}

	entryTLS := res.Server.HTTP.TLS.EntryPoints["k8s-"+strconv.Itoa(int(l.Port))]
	if certs := entryTLS.Store["secure.example.com"]; len(certs) != 1 || certs[0].Cert == "" {
		t.Fatalf("tls store = %v", entryTLS.Store)
	}

	for _, r := range res.Server.HTTP.Routers {
		if r.TLS == nil {
			t.Fatalf("router %v is not tls", r)
		}
	}

	st := res.Gateways[nsName{"app", "gw"}]
	if c := condition(t, st.Listeners[1].Conditions, string(gwv1.ListenerConditionResolvedRefs)); c.Status != metav1.ConditionFalse ||
		c.Reason != string(gwv1.ListenerReasonInvalidCertificateRef) {
		t.Fatalf("missing secret listener = %v", c)
	}

	if c := condition(t, st.Listeners[0].Conditions, string(gwv1.ListenerConditionProgrammed)); c.Status != metav1.ConditionTrue {
		t.Fatalf("valid listener = %v", c)
	}
}

func TestTranslateUnsupportedListener(t *testing.T) {
	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways: []*gwv1.Gateway{gatewayObj("app", "gw", gwv1.Listener{
			Name: "sctp", Port: 9000, Protocol: "SCTP",
		})},
	})

	st := res.Gateways[nsName{"app", "gw"}]
	if c := condition(t, st.Listeners[0].Conditions, string(gwv1.ListenerConditionAccepted)); c.Reason != string(gwv1.ListenerReasonUnsupportedProtocol) {
		t.Fatalf("listener = %v", c)
	}

	if c := condition(t, st.Conditions, string(gwv1.GatewayConditionAccepted)); c.Status != metav1.ConditionFalse {
		t.Fatalf("gateway = %v", c)
	}

	if len(res.Server.EntryPoints) != 0 {
		t.Fatalf("invalid listener got an entrypoint: %v", res.Server.EntryPoints)
	}
}

func TestTranslateMissingBackend(t *testing.T) {
	route := routeObj("app", "r", "gw", nil, backendRule("/", "nope", 80))

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes:     []*gwv1.HTTPRoute{route},
	})

	c := condition(t, res.HTTPRouteStatus(key(route))[0].Conditions, string(gwv1.RouteConditionResolvedRefs))
	if c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.RouteReasonBackendNotFound) {
		t.Fatalf("resolved = %v", c)
	}

	// the route still answers, with 500
	g := res.Server.HTTP.Middlewares[sanitize("k8s", "k8s-80", "")].Gateway
	if g == nil {
		t.Fatalf("middlewares = %v", res.Server.HTTP.Middlewares)
	}

	mw, err := g.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mw(nil).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTranslateSharedPort(t *testing.T) {
	svc, slice := serviceObj("app", "web", 80)

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways: []*gwv1.Gateway{
			gatewayObj("app", "a", httpListener("http", 80, "a.example.com")),
			gatewayObj("app", "b", httpListener("http", 80, "b.example.com")),
		},
		HTTPRoutes: []*gwv1.HTTPRoute{
			routeObj("app", "ra", "a", nil, backendRule("/", "web", 80)),
			routeObj("app", "rb", "b", nil, backendRule("/", "web", 80)),
		},
		Services:       []*corev1.Service{svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{slice},
	})

	if len(res.Server.EntryPoints) != 1 {
		t.Fatalf("entrypoints = %v", res.Server.EntryPoints)
	}

	hosts := map[string]bool{}
	for _, r := range res.Server.HTTP.Routers {
		hosts[r.Host] = true
	}

	if !hosts["a.example.com"] || !hosts["b.example.com"] {
		t.Fatalf("routers = %v", res.Server.HTTP.Routers)
	}

	// both routes share one service middleware
	services := 0
	for _, m := range res.Server.HTTP.Middlewares {
		if m.ServiceMiddleware != nil {
			services++
		}
	}

	if services != 1 {
		t.Fatalf("service middlewares = %d", services)
	}
}

func TestTranslateControllerIsolatesGateways(t *testing.T) {
	a := gatewayObj("app", "a", gwv1.Listener{Name: "udp", Port: 443, Protocol: gwv1.UDPProtocolType})
	b := gatewayObj("app", "b", gwv1.Listener{Name: "tls", Port: 443, Protocol: gwv1.TLSProtocolType, TLS: &gwv1.ListenerTLSConfig{Mode: ptr(gwv1.TLSModePassthrough)}})
	c := gatewayObj("app", "c", httpListener("http", 443, ""))

	snap := &Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{a, b, c}}

	res := Translate(&Config{Mode: ModeController}, snap)
	for _, gw := range []string{"a", "b", "c"} {
		if c := condition(t, res.Gateways[nsName{"app", gw}].Listeners[0].Conditions, string(gwv1.ListenerConditionProgrammed)); c.Status != metav1.ConditionTrue {
			t.Errorf("controller mode gateway %s: %v", gw, c)
		}
	}

	// standalone shares sockets: tls and http on 443 conflict
	res = Translate(&Config{}, snap)
	if c := condition(t, res.Gateways[nsName{"app", "c"}].Listeners[0].Conditions, string(gwv1.ListenerConditionAccepted)); c.Reason != string(gwv1.ListenerReasonProtocolConflict) {
		t.Errorf("standalone conflict: %v", c)
	}
}
