package kubernetes

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/gateway"
)

// Certificates of one hostname on several ports must be stored in a stable
// order, otherwise every sync sees a changed configuration.
func TestTranslateTLSStoreDeterministic(t *testing.T) {
	a := tlsSecret(t, "app", "a", "example.com")
	b := tlsSecret(t, "app", "b", "example.com")

	l1 := httpsListener("one", "example.com", gwv1.SecretObjectReference{Name: "a"})
	l2 := httpsListener("two", "example.com", gwv1.SecretObjectReference{Name: "b"})
	l2.Port = 8443

	snap := &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", l1, l2)},
		Secrets:        []*corev1.Secret{a, b},
	}

	first := Translate(&Config{}, snap).Server
	for range 50 {
		if !reflect.DeepEqual(first, Translate(&Config{}, snap).Server) {
			t.Fatal("translation is not deterministic")
		}
	}
}

// An unsupported regular expression must only affect its route, a failing
// gateway middleware would reject the configuration of every route.
func TestInvalidRegexMatchIsDropped(t *testing.T) {
	bad := backendRule("/", "web", 80)
	bad.Matches = []gwv1.HTTPRouteMatch{{Path: &gwv1.HTTPPathMatch{Type: ptr(gwv1.PathMatchRegularExpression), Value: ptr("/(?=x)")}}}
	mixed := backendRule("/ok", "web", 80)
	mixed.Matches = append(mixed.Matches, bad.Matches...)

	snap := &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes:     []*gwv1.HTTPRoute{routeObj("app", "r", "gw", nil, bad, mixed)},
	}

	res := Translate(&Config{}, snap)

	var rules []gateway.Rule
	for _, m := range res.Server.HTTP.Middlewares {
		if m.Gateway == nil || m.Gateway.Passthrough {
			continue
		}
		if _, err := (&gateway.Gateway{Rules: m.Gateway.Rules}).Middleware(); err != nil {
			t.Fatalf("gateway middleware fails: %v", err)
		}
		rules = append(rules, m.Gateway.Rules...)
	}

	// the rule with only an invalid match must not turn into a catch-all
	if len(rules) != 1 || len(rules[0].Matches) != 1 || rules[0].Matches[0].Path.Value != "/ok" {
		t.Fatalf("rules = %+v", rules)
	}

	st := res.HTTPRouteStatus(nsName{Namespace: "app", Name: "r"})
	if len(st) != 1 || !slices.ContainsFunc(st[0].Conditions, func(c metav1.Condition) bool {
		return c.Type == string(gwv1.RouteConditionResolvedRefs) && c.Reason == string(gwv1.RouteReasonUnsupportedValue)
	}) {
		t.Fatalf("status = %+v", st)
	}
}

// Create may race with the informer cache; the existing object is then
// updated instead of failing the sync.
func TestUpsertCreateAlreadyExists(t *testing.T) {
	ctx := context.Background()
	lbls := map[string]string{labelManagedBy: managedByValue, labelController: "id", labelGatewayName: "gw", labelGatewayNamespace: "app"}
	existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "system", Labels: lbls}, Data: map[string]string{"k": "old"}}
	kube := kubefake.NewSimpleClientset(existing)

	notCached := func(string) (*corev1.ConfigMap, error) {
		return nil, apierrors.NewNotFound(corev1.Resource("configmaps"), "cm")
	}
	want := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "system", Labels: lbls}, Data: map[string]string{"k": "new"}}

	err := upsert(ctx, kube.CoreV1().ConfigMaps("system"), notCached, want, func(cur *corev1.ConfigMap) bool {
		if cur.Data["k"] == "new" {
			return false
		}
		cur.Data = want.Data
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	cm, err := kube.CoreV1().ConfigMaps("system").Get(ctx, "cm", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cm.Data["k"] != "new" {
		t.Fatalf("configmap not updated: %v", cm.Data)
	}
}

// Annotations written by others (cloud load balancer controllers) must
// survive, while removed infrastructure annotations are removed.
func TestProxyServiceKeepsForeignAnnotations(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Image: "example:1"}}}
	gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
	gw.Spec.Infrastructure = &gwv1.GatewayInfrastructure{Annotations: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
		"lb.example.com/internal": "true",
		"lb.example.com/scheme":   "internal",
	}}
	name := proxyName(key(gw))

	if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
		t.Fatal(err)
	}

	svc, err := kube.CoreV1().Services("system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	svc.Annotations["cloud.example.com/load-balancer-id"] = "lb-1"
	if _, err := kube.CoreV1().Services("system").Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	kube.ClearActions()
	if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, action := range kube.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "services" {
			t.Error("foreign annotation caused an update")
		}
	}

	delete(gw.Spec.Infrastructure.Annotations, "lb.example.com/scheme")
	if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
		t.Fatal(err)
	}

	svc, err = kube.CoreV1().Services("system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Annotations["cloud.example.com/load-balancer-id"] != "lb-1" {
		t.Error("foreign annotation removed")
	}
	if _, ok := svc.Annotations["lb.example.com/scheme"]; ok {
		t.Error("removed infrastructure annotation kept")
	}
	if svc.Annotations["lb.example.com/internal"] != "true" {
		t.Error("infrastructure annotation lost")
	}
}

// A shutdown delay below half a second rounds to a zero second sleep, which
// older API servers reject; no preStop hook is set then.
func TestProxyDeploymentSubSecondShutdownDelay(t *testing.T) {
	p := &Provider{cfg: Config{Proxy: Proxy{Image: "turna:test", ShutdownDelay: 300 * time.Millisecond}}}
	gw := gatewayObj("app", "gw", httpListener("http", 80, ""))

	dep := p.proxyDeployment("system", "proxy", proxyLabels(key(gw)), gw, nil, "hash")
	if lc := dep.Spec.Template.Spec.Containers[0].Lifecycle; lc != nil {
		t.Fatalf("lifecycle = %+v", lc)
	}
}

// Gateway fields that change security or placement must not be ignored: the
// gateway is rejected instead of serving without them.
func TestUnsupportedGatewayFieldsReject(t *testing.T) {
	tests := map[string]struct {
		mutate func(*gwv1.Gateway)
		reason gwv1.GatewayConditionReason
	}{
		"parametersRef": {
			mutate: func(gw *gwv1.Gateway) {
				gw.Spec.Infrastructure = &gwv1.GatewayInfrastructure{ParametersRef: &gwv1.LocalParametersReference{Kind: "ConfigMap", Name: "p"}}
			},
			reason: gwv1.GatewayReasonInvalidParameters,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			gw := gatewayObj("app", "gw", httpListener("http", 80, ""))
			tt.mutate(gw)

			res := Translate(&Config{}, &Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{gw}})

			if len(res.Server.EntryPoints) != 0 {
				t.Fatalf("rejected gateway serves: %v", res.Server.EntryPoints)
			}

			st := res.Gateways[key(gw)]
			if c := condition(t, st.Conditions, string(gwv1.GatewayConditionAccepted)); c.Status != metav1.ConditionFalse || c.Reason != string(tt.reason) {
				t.Fatalf("accepted = %+v", c)
			}
		})
	}
}

// An HTTPS listener whose client certificate validation cannot be resolved
// must not serve without it; other ports keep serving.
func TestFrontendValidationRejectsListener(t *testing.T) {
	secret := tlsSecret(t, "app", "cert", "example.com")
	gw := gatewayObj("app", "gw",
		httpsListener("https", "example.com", gwv1.SecretObjectReference{Name: "cert"}),
		httpListener("http", 80, ""),
	)
	gw.Spec.TLS = &gwv1.GatewayTLSConfig{Frontend: &gwv1.FrontendTLSConfig{
		Default: gwv1.TLSConfig{Validation: &gwv1.FrontendTLSValidation{CACertificateRefs: []gwv1.ObjectReference{{Kind: "ConfigMap", Name: "ca"}}}},
	}}

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		Secrets:        []*corev1.Secret{secret},
	})

	if _, ok := res.Server.EntryPoints["k8s-443"]; ok {
		t.Fatal("listener requiring client validation is served")
	}
	if _, ok := res.Server.EntryPoints["k8s-80"]; !ok {
		t.Fatal("plain HTTP listener is not served")
	}

	li := res.Gateways[key(gw)].Listeners[0]
	if c := condition(t, li.Conditions, string(gwv1.ListenerConditionAccepted)); c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.ListenerReasonNoValidCACertificate) {
		t.Fatalf("accepted = %+v", c)
	}
}

func TestRuleTimeout(t *testing.T) {
	d := func(s string) *gwv1.Duration { v := gwv1.Duration(s); return &v }

	tests := []struct {
		in   *gwv1.HTTPRouteTimeouts
		want time.Duration
	}{
		{nil, 0},
		{&gwv1.HTTPRouteTimeouts{Request: d("10s")}, 10 * time.Second},
		{&gwv1.HTTPRouteTimeouts{BackendRequest: d("2s")}, 2 * time.Second},
		{&gwv1.HTTPRouteTimeouts{Request: d("10s"), BackendRequest: d("2s")}, 2 * time.Second},
		{&gwv1.HTTPRouteTimeouts{Request: d("0s"), BackendRequest: d("3s")}, 3 * time.Second},
	}

	for _, tt := range tests {
		if got := ruleTimeout(tt.in); got != tt.want {
			t.Errorf("ruleTimeout(%+v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
