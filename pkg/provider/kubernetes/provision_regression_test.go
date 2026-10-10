package kubernetes

import (
	"context"
	"maps"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestProxyInfrastructureCannotOverrideIdentity(t *testing.T) {
	p := &Provider{cfg: Config{Proxy: Proxy{Image: "example:1"}}}
	gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
	lbls := proxyLabels(key(gw))
	lbls[labelController] = p.controllerID()
	gw.Spec.Infrastructure = &gwv1.GatewayInfrastructure{Labels: map[gwv1.LabelKey]gwv1.LabelValue{
		"app.kubernetes.io/instance":   "other-proxy",
		gwv1.LabelKey(labelController): "other-controller",
		"team":                         "platform",
	}}
	dep := p.proxyDeployment("system", proxyName(key(gw)), lbls, gw, listenerPorts(gw, 0, 0), "hash")
	for k, v := range lbls {
		if dep.Spec.Template.Labels[k] != v {
			t.Errorf("managed label %s = %q, want %q", k, dep.Spec.Template.Labels[k], v)
		}
	}
	if dep.Spec.Template.Labels["team"] != "platform" {
		t.Fatal("custom infrastructure label lost")
	}
}

func TestProxyServiceTypeTransitions(t *testing.T) {
	for _, typ := range []corev1.ServiceType{corev1.ServiceTypeClusterIP, corev1.ServiceTypeNodePort} {
		t.Run(string(typ), func(t *testing.T) {
			ctx := context.Background()
			kube := kubefake.NewSimpleClientset()
			p := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Image: "example:1", ServiceType: "LoadBalancer"}}}
			gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
			name := proxyName(key(gw))
			if err := p.applyProxy(ctx, "system", name, gw, nil); err != nil {
				t.Fatal(err)
			}
			svc, err := kube.CoreV1().Services("system").Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			svc.Spec.Ports[0].NodePort = 30080
			svc.Spec.HealthCheckNodePort = 30999
			svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
			svc.Spec.AllocateLoadBalancerNodePorts = ptr(true)
			svc.Spec.LoadBalancerClass = ptr("example.com/lb")
			if _, err := kube.CoreV1().Services("system").Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			p.cfg.Proxy.ServiceType = string(typ)
			if err := p.applyProxy(ctx, "system", name, gw, nil); err != nil {
				t.Fatal(err)
			}
			svc, err = kube.CoreV1().Services("system").Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if svc.Spec.HealthCheckNodePort != 0 || svc.Spec.AllocateLoadBalancerNodePorts != nil || svc.Spec.LoadBalancerClass != nil {
				t.Errorf("load balancer fields survived transition: %+v", svc.Spec)
			}
			if typ == corev1.ServiceTypeClusterIP {
				if svc.Spec.Ports[0].NodePort != 0 || svc.Spec.ExternalTrafficPolicy != "" {
					t.Errorf("external traffic fields survived ClusterIP transition: %+v", svc.Spec)
				}
			} else if svc.Spec.Ports[0].NodePort != 30080 {
				t.Error("allocated NodePort lost")
			}
			kube.ClearActions()
			if err := p.applyProxy(ctx, "system", name, gw, nil); err != nil {
				t.Fatal(err)
			}
			for _, action := range kube.Actions() {
				if action.GetVerb() == "update" {
					t.Errorf("unchanged resource updated: %s", action.GetResource().Resource)
				}
			}
		})
	}
}

func TestLabelsDistinguishMissingEmptyValues(t *testing.T) {
	if maps.Equal(map[string]string{"team": ""}, map[string]string{"owner": ""}) {
		t.Fatal("different labels with empty values compare equal")
	}
}
