package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
)

func withBackendClientCert(gw *gwv1.Gateway, ref gwv1.SecretObjectReference) *gwv1.Gateway {
	gw.Spec.TLS = &gwv1.GatewayTLSConfig{Backend: &gwv1.GatewayBackendTLS{ClientCertificateRef: &ref}}

	return gw
}

// TLS backends of a Gateway get the client certificate of the Gateway.
func TestBackendClientCertificate(t *testing.T) {
	client := tlsSecret(t, "app", "client", "client.example.com")
	svc, slice := serviceObj("app", "web", 443)

	gw := withBackendClientCert(gatewayObj("app", "gw", httpListener("http", 80, "")), gwv1.SecretObjectReference{Name: "client"})
	plain := gatewayObj("app", "plain", httpListener("http", 81, ""))

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw, plain},
		HTTPRoutes: []*gwv1.HTTPRoute{
			routeObj("app", "r", "gw", []string{"a.example.com"}, backendRule("/", "web", 443)),
			routeObj("app", "p", "plain", []string{"b.example.com"}, backendRule("/", "web", 443)),
		},
		Services:           []*corev1.Service{svc},
		EndpointSlices:     []*discoveryv1.EndpointSlice{slice},
		BackendTLSPolicies: []*gwv1.BackendTLSPolicy{tlsPolicy("p", "web", "", "ca", 1)},
		ConfigMaps:         []*corev1.ConfigMap{caConfigMap(t, "app", "ca")},
		Secrets:            []*corev1.Secret{client},
	})

	st := res.Gateways[key(gw)]
	if c := meta.FindStatusCondition(st.Conditions, string(gwv1.GatewayConditionResolvedRefs)); c == nil || c.Status != metav1.ConditionTrue {
		t.Fatalf("resolved refs = %+v", c)
	}

	if c := condition(t, st.Conditions, string(gwv1.GatewayConditionAccepted)); c.Status != metav1.ConditionTrue {
		t.Fatalf("accepted = %+v", c)
	}

	// the gateway without a certificate reports no gateway ResolvedRefs
	if c := meta.FindStatusCondition(res.Gateways[key(plain)].Conditions, string(gwv1.GatewayConditionResolvedRefs)); c != nil {
		t.Errorf("plain gateway resolved refs = %+v", c)
	}

	var with, without []*service.UpstreamTLS
	for _, m := range res.Server.HTTP.Middlewares {
		if m.ServiceMiddleware == nil || m.ServiceMiddleware.TLS == nil {
			continue
		}

		if m.ServiceMiddleware.TLS.ClientCert != "" {
			with = append(with, m.ServiceMiddleware.TLS)
		} else {
			without = append(without, m.ServiceMiddleware.TLS)
		}
	}

	if len(with) != 1 || with[0].ClientCert != string(client.Data[corev1.TLSCertKey]) || with[0].ClientKey != string(client.Data[corev1.TLSPrivateKeyKey]) {
		t.Fatalf("services with client certificate = %+v", with)
	}

	// the other gateway shares the backend but presents no certificate
	if len(without) != 1 {
		t.Fatalf("services without client certificate = %+v", without)
	}

	// the proxy gets the certificate in its bundle
	bundle := bundleCertificates(gw, newIndex(&Snapshot{Secrets: []*corev1.Secret{client}}))
	if string(bundle["app_client_"+corev1.TLSCertKey]) != string(client.Data[corev1.TLSCertKey]) {
		t.Fatalf("bundle = %v", bundle)
	}
}

// Invalid references keep the Gateway accepted and report ResolvedRefs.
func TestBackendClientCertificateInvalid(t *testing.T) {
	malformed := &corev1.Secret{ObjectMeta: objMeta("app", "malformed"), Type: corev1.SecretTypeOpaque, Data: map[string][]byte{}}
	other := tlsSecret(t, "other", "cert", "x.example.com")

	for name, tt := range map[string]struct {
		ref    gwv1.SecretObjectReference
		reason gwv1.GatewayConditionReason
	}{
		"missing":   {gwv1.SecretObjectReference{Name: "missing"}, gwv1.GatewayReasonInvalidClientCertificateRef},
		"group":     {gwv1.SecretObjectReference{Group: ptr(gwv1.Group("wrong.example.com")), Name: "malformed"}, gwv1.GatewayReasonInvalidClientCertificateRef},
		"kind":      {gwv1.SecretObjectReference{Kind: ptr(gwv1.Kind("WrongKind")), Name: "malformed"}, gwv1.GatewayReasonInvalidClientCertificateRef},
		"malformed": {gwv1.SecretObjectReference{Name: "malformed"}, gwv1.GatewayReasonInvalidClientCertificateRef},
		"no grant":  {gwv1.SecretObjectReference{Name: "cert", Namespace: ptr(gwv1.Namespace("other"))}, gwv1.GatewayReasonRefNotPermitted},
	} {
		t.Run(name, func(t *testing.T) {
			gw := withBackendClientCert(gatewayObj("app", "gw", httpListener("http", 80, "")), tt.ref)

			res := Translate(&Config{}, &Snapshot{
				GatewayClasses: []*gwv1.GatewayClass{class()},
				Gateways:       []*gwv1.Gateway{gw},
				Secrets:        []*corev1.Secret{malformed, other},
			})

			st := res.Gateways[key(gw)]
			if c := meta.FindStatusCondition(st.Conditions, string(gwv1.GatewayConditionResolvedRefs)); c == nil || c.Status != metav1.ConditionFalse || c.Reason != string(tt.reason) {
				t.Fatalf("resolved refs = %+v", c)
			}

			if c := condition(t, st.Conditions, string(gwv1.GatewayConditionAccepted)); c.Status != metav1.ConditionTrue || c.Reason != string(gwv1.GatewayReasonAccepted) {
				t.Fatalf("accepted = %+v", c)
			}

			if _, ok := res.Server.EntryPoints["k8s-80"]; !ok {
				t.Fatal("gateway with an invalid backend client certificate is not served")
			}

			if len(bundleCertificates(gw, newIndex(&Snapshot{Secrets: []*corev1.Secret{other}}))) != 0 && name == "no grant" {
				t.Fatal("certificate without a grant bundled")
			}
		})
	}
}
