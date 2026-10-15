package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
)

func caConfigMap(t *testing.T, ns, name string) *corev1.ConfigMap {
	t.Helper()

	return &corev1.ConfigMap{ObjectMeta: objMeta(ns, name), Data: map[string]string{caCertificateKey: testCAPEM(t)}}
}

func frontendCAs(mode gwv1.FrontendValidationModeType, refs ...gwv1.ObjectReference) *gwv1.FrontendTLSValidation {
	return &gwv1.FrontendTLSValidation{CACertificateRefs: refs, Mode: mode}
}

func configMapRef(name string) gwv1.ObjectReference {
	return gwv1.ObjectReference{Kind: kindConfigMap, Name: gwv1.ObjectName(name)}
}

// The default and per port validation of a Gateway become the client
// certificate settings of the HTTPS entrypoints.
func TestFrontendClientValidation(t *testing.T) {
	secret := tlsSecret(t, "app", "cert", "example.com")
	ca, perPort := caConfigMap(t, "app", "ca"), caConfigMap(t, "app", "per-port")

	second := httpsListener("second", "second.example.com", gwv1.SecretObjectReference{Name: "cert"})
	second.Port = 8443

	gw := gatewayObj("app", "gw", httpsListener("https", "example.com", gwv1.SecretObjectReference{Name: "cert"}), second, httpListener("http", 80, ""))
	gw.Spec.TLS = &gwv1.GatewayTLSConfig{Frontend: &gwv1.FrontendTLSConfig{
		Default: gwv1.TLSConfig{Validation: frontendCAs("", configMapRef("ca"))},
		PerPort: []gwv1.TLSPortConfig{{Port: 8443, TLS: gwv1.TLSConfig{Validation: frontendCAs(gwv1.AllowInsecureFallback, configMapRef("per-port"))}}},
	}}

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		Secrets:        []*corev1.Secret{secret},
		ConfigMaps:     []*corev1.ConfigMap{ca, perPort},
	})

	want := map[string]shttp.EntryPointTLS{
		"k8s-443":  {ClientCAs: []string{ca.Data[caCertificateKey]}, ClientAuth: shttp.ClientAuthRequire},
		"k8s-8443": {ClientCAs: []string{perPort.Data[caCertificateKey]}, ClientAuth: shttp.ClientAuthRequest},
	}

	got := res.Server.HTTP.TLS.EntryPoints
	if len(got) != len(want) {
		t.Fatalf("entrypoints = %+v", got)
	}

	for name, w := range want {
		g := got[name]
		if g.ClientAuth != w.ClientAuth || len(g.ClientCAs) != 1 || g.ClientCAs[0] != w.ClientCAs[0] {
			t.Errorf("%s = %+v, want %+v", name, g, w)
		}
	}

	st := res.Gateways[key(gw)]
	for _, li := range st.Listeners {
		for _, typ := range []string{string(gwv1.ListenerConditionAccepted), string(gwv1.ListenerConditionResolvedRefs)} {
			if c := condition(t, li.Conditions, typ); c.Status != metav1.ConditionTrue {
				t.Errorf("listener %s %s = %+v", li.Name, typ, c)
			}
		}
	}

	if c := meta.FindStatusCondition(st.Conditions, string(gwv1.GatewayConditionInsecureFrontendValidationMode)); c == nil || c.Status != metav1.ConditionTrue ||
		c.Reason != string(gwv1.GatewayReasonConfigurationChanged) {
		t.Errorf("insecure frontend condition = %+v", c)
	}
}

// Invalid references affect only the listeners using them, with the
// reasons of the specification.
func TestFrontendClientValidationInvalid(t *testing.T) {
	secret := tlsSecret(t, "app", "cert", "example.com")

	listener := func(name string, port int32) gwv1.Listener {
		l := httpsListener(name, name+".example.com", gwv1.SecretObjectReference{Name: "cert"})
		l.Port = gwv1.PortNumber(port)

		return l
	}

	other := gwv1.ObjectReference{Kind: kindConfigMap, Name: "other-ca", Namespace: ptr(gwv1.Namespace("other"))}

	gw := gatewayObj("app", "gw",
		listener("valid", 443), listener("unresolved", 7443), listener("kind", 8443),
		listener("grant", 6443), listener("granted", 5443), httpListener("http", 80, ""))
	gw.Spec.TLS = &gwv1.GatewayTLSConfig{Frontend: &gwv1.FrontendTLSConfig{
		Default: gwv1.TLSConfig{Validation: frontendCAs("", configMapRef("ca"))},
		PerPort: []gwv1.TLSPortConfig{
			{Port: 7443, TLS: gwv1.TLSConfig{Validation: frontendCAs("", configMapRef("missing"))}},
			{Port: 8443, TLS: gwv1.TLSConfig{Validation: frontendCAs("", gwv1.ObjectReference{Kind: "Service", Name: "svc"})}},
			{Port: 6443, TLS: gwv1.TLSConfig{Validation: frontendCAs("", other)}},
			{Port: 5443, TLS: gwv1.TLSConfig{Validation: frontendCAs("", gwv1.ObjectReference{Kind: kindConfigMap, Name: "granted-ca", Namespace: ptr(gwv1.Namespace("other"))})}},
			// a validation for a plain HTTP port is ignored
			{Port: 80, TLS: gwv1.TLSConfig{Validation: frontendCAs("", configMapRef("missing"))}},
		},
	}}

	grant := &gwv1.ReferenceGrant{
		ObjectMeta: objMeta("other", "grant"),
		Spec: gwv1.ReferenceGrantSpec{
			From: []gwv1.ReferenceGrantFrom{{Group: groupGateway, Kind: kindGateway, Namespace: "app"}},
			To:   []gwv1.ReferenceGrantTo{{Kind: kindConfigMap, Name: ptr(gwv1.ObjectName("granted-ca"))}},
		},
	}

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses:  []*gwv1.GatewayClass{class()},
		Gateways:        []*gwv1.Gateway{gw},
		Secrets:         []*corev1.Secret{secret},
		ConfigMaps:      []*corev1.ConfigMap{caConfigMap(t, "app", "ca"), caConfigMap(t, "other", "other-ca"), caConfigMap(t, "other", "granted-ca")},
		ReferenceGrants: []*gwv1.ReferenceGrant{grant},
	})

	byName := map[string]gwv1.ListenerStatus{}
	for _, li := range res.Gateways[key(gw)].Listeners {
		byName[string(li.Name)] = li
	}

	for _, tt := range []struct {
		listener, resolved, accepted string
		ok                           bool
	}{
		{"valid", "", "", true},
		{"granted", "", "", true},
		{"http", "", "", true},
		{"unresolved", string(gwv1.ListenerReasonInvalidCACertificateRef), string(gwv1.ListenerReasonNoValidCACertificate), false},
		{"kind", string(gwv1.ListenerReasonInvalidCACertificateKind), string(gwv1.ListenerReasonNoValidCACertificate), false},
		{"grant", string(gwv1.ListenerReasonRefNotPermitted), string(gwv1.ListenerReasonNoValidCACertificate), false},
	} {
		li := byName[tt.listener]
		resolved := condition(t, li.Conditions, string(gwv1.ListenerConditionResolvedRefs))
		accepted := condition(t, li.Conditions, string(gwv1.ListenerConditionAccepted))
		programmed := condition(t, li.Conditions, string(gwv1.ListenerConditionProgrammed))

		if tt.ok {
			if resolved.Status != metav1.ConditionTrue || accepted.Status != metav1.ConditionTrue || programmed.Status != metav1.ConditionTrue {
				t.Errorf("%s: resolved %+v accepted %+v programmed %+v", tt.listener, resolved, accepted, programmed)
			}

			continue
		}

		if resolved.Status != metav1.ConditionFalse || resolved.Reason != tt.resolved {
			t.Errorf("%s resolved = %+v, want %s", tt.listener, resolved, tt.resolved)
		}

		if accepted.Status != metav1.ConditionFalse || accepted.Reason != tt.accepted {
			t.Errorf("%s accepted = %+v, want %s", tt.listener, accepted, tt.accepted)
		}

		if programmed.Status != metav1.ConditionFalse {
			t.Errorf("%s programmed = %+v", tt.listener, programmed)
		}
	}

	// invalid listeners are not served, they never accept clients without
	// the required certificates
	for _, entry := range []string{"k8s-7443", "k8s-8443", "k8s-6443"} {
		if _, ok := res.Server.EntryPoints[entry]; ok {
			t.Errorf("%s is served", entry)
		}
	}

	for _, entry := range []string{"k8s-443", "k8s-5443", "k8s-80"} {
		if _, ok := res.Server.EntryPoints[entry]; !ok {
			t.Errorf("%s is not served", entry)
		}
	}

	if _, ok := res.Server.HTTP.TLS.EntryPoints["k8s-80"]; ok {
		t.Error("client validation on a plain HTTP entrypoint")
	}

	if meta.FindStatusCondition(res.Gateways[key(gw)].Conditions, string(gwv1.GatewayConditionInsecureFrontendValidationMode)) != nil {
		t.Error("insecure frontend condition without AllowInsecureFallback")
	}
}

// Proxies get the client CA certificates of their gateway in the bundle,
// only when the reference is permitted.
func TestBundleClientCACertificates(t *testing.T) {
	gw := gatewayObj("app", "gw")
	gw.Spec.TLS = &gwv1.GatewayTLSConfig{Frontend: &gwv1.FrontendTLSConfig{
		Default: gwv1.TLSConfig{Validation: frontendCAs("", configMapRef("ca"), gwv1.ObjectReference{Kind: kindConfigMap, Name: "x", Namespace: ptr(gwv1.Namespace("other"))})},
	}}

	idx := newIndex(&Snapshot{ConfigMaps: []*corev1.ConfigMap{
		{ObjectMeta: objMeta("app", "ca"), Data: map[string]string{caCertificateKey: "A"}},
		{ObjectMeta: objMeta("other", "x"), Data: map[string]string{caCertificateKey: "X"}},
	}})

	got := bundleClientCACertificates(gw, idx)
	if len(got) != 1 || string(got[caBundlePrefix+"app_ca"]) != "A" {
		t.Fatalf("bundle = %v", got)
	}

	if cms := unbundleConfigMaps(&corev1.Secret{Data: got}); len(cms) != 1 || cms[0].Namespace != "app" || cms[0].Name != "ca" {
		t.Fatalf("unbundled = %+v", cms)
	}
}
