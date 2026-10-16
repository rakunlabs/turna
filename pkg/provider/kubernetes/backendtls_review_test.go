package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// A policy that loses one of its targets is reported conflicted only to the
// gateways using that target; its other targets still apply.
func TestBackendTLSPolicyPartialConflict(t *testing.T) {
	ca := testCAPEM(t)

	shared, sharedSlice := serviceObj("app", "shared", 443)
	own, ownSlice := serviceObj("app", "own", 443)

	older := tlsPolicy("older", "shared", "", "ca", 1)
	older.Spec.Validation.Hostname = "older.example.com"

	newer := tlsPolicy("newer", "shared", "", "ca", 2)
	newer.Spec.Validation.Hostname = "newer.example.com"
	newer.Spec.TargetRefs = append(newer.Spec.TargetRefs, gwv1.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: gwv1.LocalPolicyTargetReference{Kind: kindService, Name: "own"},
	})

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways: []*gwv1.Gateway{
			gatewayObj("app", "gw-shared", httpListener("http", 80, "")),
			gatewayObj("app", "gw-own", httpListener("http", 80, "")),
		},
		HTTPRoutes: []*gwv1.HTTPRoute{
			routeObj("app", "r-shared", "gw-shared", nil, backendRule("/", "shared", 443)),
			routeObj("app", "r-own", "gw-own", nil, backendRule("/", "own", 443)),
		},
		Services:           []*corev1.Service{shared, own},
		EndpointSlices:     []*discoveryv1.EndpointSlice{sharedSlice, ownSlice},
		BackendTLSPolicies: []*gwv1.BackendTLSPolicy{newer, older},
		ConfigMaps:         []*corev1.ConfigMap{{ObjectMeta: objMeta("app", "ca"), Data: map[string]string{caCertificateKey: ca}}},
	})

	accepted := map[string]metav1.Condition{}
	for _, a := range res.PolicyAncestors(nsName{"app", "newer"}) {
		accepted[string(a.AncestorRef.Name)] = *meta.FindStatusCondition(a.Conditions, string(gwv1.PolicyConditionAccepted))
	}

	if c := accepted["gw-shared"]; c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.PolicyReasonConflicted) {
		t.Fatalf("gw-shared accepted = %+v", c)
	}

	if c := accepted["gw-own"]; c.Status != metav1.ConditionTrue {
		t.Fatalf("gw-own accepted = %+v", c)
	}

	names := map[string]bool{}
	for _, cfg := range serviceTLS(res, "any") {
		names[cfg.ServerName] = true
	}

	if !names["older.example.com"] || !names["newer.example.com"] || len(names) != 2 {
		t.Fatalf("server names = %v", names)
	}

	// each proxy bundles only the policies applied to its backends
	if _, ok := res.gatewayPolicies[nsName{"app", "gw-own"}][nsName{"app", "newer"}]; !ok || len(res.gatewayPolicies[nsName{"app", "gw-own"}]) != 1 {
		t.Fatalf("gw-own policies = %v", res.gatewayPolicies[nsName{"app", "gw-own"}])
	}

	if _, ok := res.gatewayPolicies[nsName{"app", "gw-shared"}][nsName{"app", "older"}]; !ok || len(res.gatewayPolicies[nsName{"app", "gw-shared"}]) != 1 {
		t.Fatalf("gw-shared policies = %v", res.gatewayPolicies[nsName{"app", "gw-shared"}])
	}
}

// The status of a policy used by many gateways stays within the CRD limit.
func TestBackendTLSPolicyAncestorLimit(t *testing.T) {
	ca := testCAPEM(t)
	svc, slice := serviceObj("app", "web", 443)

	snap := &Snapshot{
		GatewayClasses:     []*gwv1.GatewayClass{class()},
		Services:           []*corev1.Service{svc},
		EndpointSlices:     []*discoveryv1.EndpointSlice{slice},
		BackendTLSPolicies: []*gwv1.BackendTLSPolicy{tlsPolicy("p", "web", "", "ca", 1)},
		ConfigMaps:         []*corev1.ConfigMap{{ObjectMeta: objMeta("app", "ca"), Data: map[string]string{caCertificateKey: ca}}},
	}

	for i := range maxPolicyAncestors + 4 {
		gw := fmt.Sprintf("gw-%02d", i)
		snap.Gateways = append(snap.Gateways, gatewayObj("app", gw, httpListener("http", 80, "")))
		snap.HTTPRoutes = append(snap.HTTPRoutes, routeObj("app", "r-"+gw, gw, nil, backendRule("/", "web", 443)))
	}

	res := Translate(&Config{}, snap)

	if got := len(res.PolicyAncestors(nsName{"app", "p"})); got != maxPolicyAncestors {
		t.Fatalf("ancestors = %d, want %d", got, maxPolicyAncestors)
	}
}

func TestBundleCACertificatesPerGateway(t *testing.T) {
	a := tlsPolicy("a", "web", "", "ca-a", 1)
	b := tlsPolicy("b", "web", "", "ca-b", 1)

	idx := newIndex(&Snapshot{ConfigMaps: []*corev1.ConfigMap{
		{ObjectMeta: objMeta("app", "ca-a"), Data: map[string]string{caCertificateKey: "A"}},
		{ObjectMeta: objMeta("app", "ca-b"), Data: map[string]string{caCertificateKey: "B"}},
	}})

	got := bundleCACertificates([]*gwv1.BackendTLSPolicy{a, b}, map[nsName]struct{}{{"app", "a"}: {}}, idx)
	if len(got) != 1 || string(got[caBundlePrefix+"app_ca-a"]) != "A" {
		t.Fatalf("bundle = %v", got)
	}

	if got := bundleCACertificates([]*gwv1.BackendTLSPolicy{a, b}, nil, idx); len(got) != 0 {
		t.Fatalf("bundle without policies = %v", got)
	}
}

func TestStripConfigMap(t *testing.T) {
	in := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "ca", ResourceVersion: "7", Labels: map[string]string{"a": "b"}},
		Data:       map[string]string{caCertificateKey: "CA", "dashboard.json": "large"},
		BinaryData: map[string][]byte{"blob": []byte("large")},
	}

	out, err := stripConfigMap(in)
	if err != nil {
		t.Fatal(err)
	}

	cm := out.(*corev1.ConfigMap)
	if cm.Namespace != "app" || cm.Name != "ca" || cm.ResourceVersion != "7" || len(cm.Labels) != 0 ||
		len(cm.Data) != 1 || cm.Data[caCertificateKey] != "CA" || cm.BinaryData != nil {
		t.Fatalf("stripped = %+v", cm)
	}

	other, _ := stripConfigMap(&corev1.ConfigMap{ObjectMeta: objMeta("app", "x"), Data: map[string]string{"k": "v"}})
	if other.(*corev1.ConfigMap).Data != nil {
		t.Fatalf("stripped without ca = %+v", other)
	}
}

func TestCAHandlerIgnoresUnrelatedConfigMaps(t *testing.T) {
	var calls int
	h := caHandler(func() { calls++ })

	plain := &corev1.ConfigMap{ObjectMeta: objMeta("app", "x")}
	withCA := &corev1.ConfigMap{ObjectMeta: objMeta("app", "ca"), Data: map[string]string{caCertificateKey: "A"}}
	changed := &corev1.ConfigMap{ObjectMeta: objMeta("app", "ca"), Data: map[string]string{caCertificateKey: "B"}}

	h.OnAdd(plain, false)
	h.OnUpdate(plain, plain)
	h.OnDelete(plain)

	if calls != 0 {
		t.Fatalf("unrelated ConfigMap notified %d times", calls)
	}

	h.OnAdd(withCA, false)
	h.OnUpdate(withCA, withCA)
	h.OnUpdate(withCA, changed)
	h.OnUpdate(changed, plain)
	h.OnDelete(withCA)

	if calls != 4 {
		t.Fatalf("calls = %d, want 4", calls)
	}
}

// A failed access review is not taken as permission, discovery retries.
func TestCanWatchBackendTLSReviewError(t *testing.T) {
	attrs := authorizationv1.ResourceAttributes{Group: gwv1.GroupName, Resource: "backendtlspolicies"}

	review := func(allowed bool, err error) *Provider {
		kube := kubefake.NewSimpleClientset()
		kube.PrependReactor("create", "selfsubjectaccessreviews", func(k8stesting.Action) (bool, runtime.Object, error) {
			if err != nil {
				return true, nil, err
			}

			return true, &authorizationv1.SelfSubjectAccessReview{Status: authorizationv1.SubjectAccessReviewStatus{Allowed: allowed}}, nil
		})

		return &Provider{kube: kube}
	}

	if _, err := review(false, errors.New("unavailable")).canWatch(context.Background(), "BackendTLSPolicy", attrs); err == nil {
		t.Fatal("review error was ignored")
	}

	if ok, err := review(false, nil).canWatch(context.Background(), "BackendTLSPolicy", attrs); err != nil || ok {
		t.Fatalf("denied = %v, %v", ok, err)
	}

	if ok, err := review(true, nil).canWatch(context.Background(), "BackendTLSPolicy", attrs); err != nil || !ok {
		t.Fatalf("allowed = %v, %v", ok, err)
	}
}
