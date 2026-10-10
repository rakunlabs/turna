package kubernetes

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
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

	if err := p.applyProxy(ctx, "system", name, gw, nil); err != nil {
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
	if err := p.applyProxy(ctx, "system", name, gw, nil); err != nil {
		t.Fatal(err)
	}
	for _, action := range kube.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "services" {
			t.Error("foreign annotation caused an update")
		}
	}

	delete(gw.Spec.Infrastructure.Annotations, "lb.example.com/scheme")
	if err := p.applyProxy(ctx, "system", name, gw, nil); err != nil {
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
