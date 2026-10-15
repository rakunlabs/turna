package kubernetes

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
)

func testCAPEM(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func tlsPolicy(name, svc, section, ca string, created int64) *gwv1.BackendTLSPolicy {
	p := &gwv1.BackendTLSPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: name, Generation: 1, CreationTimestamp: metav1.NewTime(time.Unix(created, 0))},
		Spec: gwv1.BackendTLSPolicySpec{
			TargetRefs: []gwv1.LocalPolicyTargetReferenceWithSectionName{{
				LocalPolicyTargetReference: gwv1.LocalPolicyTargetReference{Kind: kindService, Name: gwv1.ObjectName(svc)},
			}},
			Validation: gwv1.BackendTLSPolicyValidation{
				Hostname:          "abc.example.com",
				CACertificateRefs: []gwv1.LocalObjectReference{{Kind: kindConfigMap, Name: gwv1.ObjectName(ca)}},
			},
		},
	}

	if section != "" {
		p.Spec.TargetRefs[0].SectionName = ptr(gwv1.SectionName(section))
	}

	return p
}

func policyCondition(t *testing.T, res *Result, name, typ string) metav1.Condition {
	t.Helper()

	ancestors := res.PolicyAncestors(nsName{"app", name})
	if len(ancestors) != 1 {
		t.Fatalf("%s ancestors = %+v", name, ancestors)
	}

	if ref := ancestors[0].AncestorRef; ref.Name != "gw" || ptrOr(ref.Namespace, "") != "app" || ptrOr(ref.Kind, "") != kindGateway {
		t.Fatalf("%s ancestor = %+v", name, ref)
	}

	c := meta.FindStatusCondition(ancestors[0].Conditions, typ)
	if c == nil {
		t.Fatalf("%s has no %s condition", name, typ)
	}

	return *c
}

func serviceTLS(res *Result, svc string) []*service.UpstreamTLS {
	var out []*service.UpstreamTLS

	for _, m := range res.Server.HTTP.Middlewares {
		if m.ServiceMiddleware == nil {
			continue
		}

		for _, s := range m.ServiceMiddleware.LoadBalancer.Servers {
			if strings.HasPrefix(s.URL, "https://") && m.ServiceMiddleware.TLS != nil && svc != "" {
				out = append(out, m.ServiceMiddleware.TLS)

				break
			}
		}
	}

	return out
}

func TestBackendTLSPolicy(t *testing.T) {
	ca := testCAPEM(t)

	svc, slice := serviceObj("app", "web", 443)

	valid := tlsPolicy("valid", "web", "", "ca", 1)
	valid.Spec.Validation.SubjectAltNames = []gwv1.SubjectAltName{
		{Type: gwv1.HostnameSubjectAltNameType, Hostname: "abc.example.com"},
		{Type: gwv1.URISubjectAltNameType, URI: "spiffe://abc.example.com/id"},
	}

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses:     []*gwv1.GatewayClass{class()},
		Gateways:           []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes:         []*gwv1.HTTPRoute{routeObj("app", "r", "gw", nil, backendRule("/", "web", 443))},
		Services:           []*corev1.Service{svc},
		EndpointSlices:     []*discoveryv1.EndpointSlice{slice},
		BackendTLSPolicies: []*gwv1.BackendTLSPolicy{valid},
		ConfigMaps:         []*corev1.ConfigMap{{ObjectMeta: objMeta("app", "ca"), Data: map[string]string{caCertificateKey: ca}}},
	})

	if c := policyCondition(t, res, "valid", string(gwv1.PolicyConditionAccepted)); c.Status != metav1.ConditionTrue {
		t.Fatalf("accepted = %+v", c)
	}

	if c := policyCondition(t, res, "valid", string(gwv1.BackendTLSPolicyConditionResolvedRefs)); c.Status != metav1.ConditionTrue {
		t.Fatalf("resolved = %+v", c)
	}

	cfgs := serviceTLS(res, "web")
	if len(cfgs) != 1 {
		t.Fatalf("upstream tls = %+v", cfgs)
	}

	got := cfgs[0]
	if got.CA != ca || got.ServerName != "abc.example.com" || got.Invalid ||
		len(got.SubjectAltNames) != 2 || got.SubjectAltNames[1] != "spiffe://abc.example.com/id" {
		t.Fatalf("upstream tls = %+v", got)
	}
}

func TestBackendTLSPolicyInvalid(t *testing.T) {
	ca := testCAPEM(t)

	for _, tt := range []struct {
		name     string
		policy   func() *gwv1.BackendTLSPolicy
		cms      []*corev1.ConfigMap
		accepted string
		resolved string
	}{
		{
			name:     "missing configmap",
			policy:   func() *gwv1.BackendTLSPolicy { return tlsPolicy("p", "web", "", "missing", 1) },
			accepted: string(gwv1.BackendTLSPolicyReasonNoValidCACertificate),
			resolved: string(gwv1.BackendTLSPolicyReasonInvalidCACertificateRef),
		},
		{
			name:     "malformed configmap",
			policy:   func() *gwv1.BackendTLSPolicy { return tlsPolicy("p", "web", "", "bad", 1) },
			cms:      []*corev1.ConfigMap{{ObjectMeta: objMeta("app", "bad"), Data: map[string]string{}}},
			accepted: string(gwv1.BackendTLSPolicyReasonNoValidCACertificate),
			resolved: string(gwv1.BackendTLSPolicyReasonInvalidCACertificateRef),
		},
		{
			name: "invalid kind",
			policy: func() *gwv1.BackendTLSPolicy {
				p := tlsPolicy("p", "web", "", "ca", 1)
				p.Spec.Validation.CACertificateRefs[0].Group = "invalid.io"
				p.Spec.Validation.CACertificateRefs[0].Kind = "InvalidKind"

				return p
			},
			cms:      []*corev1.ConfigMap{{ObjectMeta: objMeta("app", "ca"), Data: map[string]string{caCertificateKey: ca}}},
			accepted: string(gwv1.BackendTLSPolicyReasonNoValidCACertificate),
			resolved: string(gwv1.BackendTLSPolicyReasonInvalidKind),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, slice := serviceObj("app", "web", 443)

			res := Translate(&Config{}, &Snapshot{
				GatewayClasses:     []*gwv1.GatewayClass{class()},
				Gateways:           []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
				HTTPRoutes:         []*gwv1.HTTPRoute{routeObj("app", "r", "gw", nil, backendRule("/", "web", 443))},
				Services:           []*corev1.Service{svc},
				EndpointSlices:     []*discoveryv1.EndpointSlice{slice},
				BackendTLSPolicies: []*gwv1.BackendTLSPolicy{tt.policy()},
				ConfigMaps:         tt.cms,
			})

			if c := policyCondition(t, res, "p", string(gwv1.PolicyConditionAccepted)); c.Status != metav1.ConditionFalse || c.Reason != tt.accepted {
				t.Fatalf("accepted = %+v", c)
			}

			if c := policyCondition(t, res, "p", string(gwv1.BackendTLSPolicyConditionResolvedRefs)); c.Status != metav1.ConditionFalse || c.Reason != tt.resolved {
				t.Fatalf("resolved = %+v", c)
			}

			// traffic must fail instead of falling back to plain text
			cfgs := serviceTLS(res, "web")
			if len(cfgs) != 1 || !cfgs[0].Invalid {
				t.Fatalf("upstream tls = %+v", cfgs)
			}
		})
	}
}

func TestBackendTLSPolicyConflict(t *testing.T) {
	ca := testCAPEM(t)

	svc, slice := serviceObj("app", "web", 443)
	svc.Spec.Ports[0].Name = "https"
	slice.Ports[0].Name = ptr("https")

	older := tlsPolicy("older", "web", "", "ca", 1)
	newer := tlsPolicy("newer", "web", "", "ca", 2)
	newer.Spec.Validation.Hostname = "newer.example.com"

	// a policy for the section takes precedence over the whole Service
	section := tlsPolicy("section", "web", "https", "ca", 3)
	section.Spec.Validation.Hostname = "section.example.com"

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses:     []*gwv1.GatewayClass{class()},
		Gateways:           []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes:         []*gwv1.HTTPRoute{routeObj("app", "r", "gw", nil, backendRule("/", "web", 443))},
		Services:           []*corev1.Service{svc},
		EndpointSlices:     []*discoveryv1.EndpointSlice{slice},
		BackendTLSPolicies: []*gwv1.BackendTLSPolicy{newer, section, older},
		ConfigMaps:         []*corev1.ConfigMap{{ObjectMeta: objMeta("app", "ca"), Data: map[string]string{caCertificateKey: ca}}},
	})

	if c := policyCondition(t, res, "section", string(gwv1.PolicyConditionAccepted)); c.Status != metav1.ConditionTrue {
		t.Fatalf("section accepted = %+v", c)
	}

	if c := policyCondition(t, res, "older", string(gwv1.PolicyConditionAccepted)); c.Status != metav1.ConditionTrue {
		t.Fatalf("older accepted = %+v", c)
	}

	if c := policyCondition(t, res, "newer", string(gwv1.PolicyConditionAccepted)); c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.PolicyReasonConflicted) {
		t.Fatalf("newer accepted = %+v", c)
	}

	cfgs := serviceTLS(res, "web")
	if len(cfgs) != 1 || cfgs[0].ServerName != "section.example.com" {
		t.Fatalf("upstream tls = %+v", cfgs)
	}
}
