package kubernetes

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func address(typ gwv1.AddressType, value string) gwv1.GatewaySpecAddress {
	a := gwv1.GatewaySpecAddress{Value: value}
	if typ != "" {
		a.Type = ptr(typ)
	}

	return a
}

func TestGatewayAddressesStatus(t *testing.T) {
	for name, tt := range map[string]struct {
		cfg        Config
		addresses  []gwv1.GatewaySpecAddress
		accepted   string
		programmed string
	}{
		"ip":                {Config{Mode: ModeController}, []gwv1.GatewaySpecAddress{address("", "192.0.2.10")}, string(gwv1.GatewayReasonAccepted), string(gwv1.GatewayReasonProgrammed)},
		"empty value":       {Config{Mode: ModeController}, []gwv1.GatewaySpecAddress{address(gwv1.IPAddressType, "")}, string(gwv1.GatewayReasonAccepted), string(gwv1.GatewayReasonProgrammed)},
		"hostname":          {Config{Mode: ModeController}, []gwv1.GatewaySpecAddress{address(gwv1.HostnameAddressType, "gw.example.com")}, string(gwv1.GatewayReasonUnsupportedAddress), string(gwv1.GatewayReasonAddressNotUsable)},
		"custom type":       {Config{Mode: ModeController}, []gwv1.GatewaySpecAddress{address("test/fake-invalid-type", "x"), address("", "192.0.2.10")}, string(gwv1.GatewayReasonUnsupportedAddress), string(gwv1.GatewayReasonAddressNotUsable)},
		"not an ip":         {Config{Mode: ModeController}, []gwv1.GatewaySpecAddress{address(gwv1.IPAddressType, "gw.example.com")}, string(gwv1.GatewayReasonUnsupportedAddress), string(gwv1.GatewayReasonAddressNotUsable)},
		"standalone":        {Config{}, []gwv1.GatewaySpecAddress{address("", "192.0.2.10")}, string(gwv1.GatewayReasonAccepted), string(gwv1.GatewayReasonAddressNotUsable)},
		"node port":         {Config{Mode: ModeController, Proxy: Proxy{ServiceType: "NodePort"}}, []gwv1.GatewaySpecAddress{address("", "192.0.2.10")}, string(gwv1.GatewayReasonAccepted), string(gwv1.GatewayReasonAddressNotUsable)},
		"two without annot": {Config{Mode: ModeController}, []gwv1.GatewaySpecAddress{address("", "192.0.2.10"), address("", "192.0.2.11")}, string(gwv1.GatewayReasonAccepted), string(gwv1.GatewayReasonAddressNotUsable)},
		"two with annot":    {Config{Mode: ModeController, Proxy: Proxy{AddressesAnnotation: "metallb.io/loadBalancerIPs"}}, []gwv1.GatewaySpecAddress{address("", "192.0.2.10"), address("", "192.0.2.11")}, string(gwv1.GatewayReasonAccepted), string(gwv1.GatewayReasonProgrammed)},
	} {
		t.Run(name, func(t *testing.T) {
			gw := gatewayObj("app", "gw", httpListener("http", 80, ""))
			gw.Spec.Addresses = tt.addresses

			res := Translate(&tt.cfg, &Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{gw}})
			st := res.Gateways[key(gw)]

			if c := condition(t, st.Conditions, string(gwv1.GatewayConditionAccepted)); c.Reason != tt.accepted {
				t.Errorf("accepted = %+v, want %s", c, tt.accepted)
			}

			if c := condition(t, st.Conditions, string(gwv1.GatewayConditionProgrammed)); c.Reason != tt.programmed {
				t.Errorf("programmed = %+v, want %s", c, tt.programmed)
			}
		})
	}
}

// The requested addresses are set on the proxy Service and updated when the
// Gateway changes; an address set by others is kept.
func TestGatewayAddressesService(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := &Provider{kube: kube, cfg: Config{Mode: ModeController, Proxy: Proxy{Namespace: "system", Image: "example:1"}}}

	gw := gatewayObj("app", "gw", httpListener("http", 80, ""))
	name := proxyName(key(gw))

	get := func() *corev1.Service {
		t.Helper()

		svc, err := kube.CoreV1().Services("system").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}

		return svc
	}

	apply := func() {
		t.Helper()

		if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	gw.Spec.Addresses = []gwv1.GatewaySpecAddress{address("", "192.0.2.10")}
	apply()

	if svc := get(); svc.Spec.LoadBalancerIP != "192.0.2.10" {
		t.Fatalf("load balancer ip = %q", svc.Spec.LoadBalancerIP)
	}

	gw.Spec.Addresses = []gwv1.GatewaySpecAddress{address("", "192.0.2.11")}
	apply()

	if svc := get(); svc.Spec.LoadBalancerIP != "192.0.2.11" {
		t.Fatalf("changed load balancer ip = %q", svc.Spec.LoadBalancerIP)
	}

	gw.Spec.Addresses = nil
	apply()

	svc := get()
	if svc.Spec.LoadBalancerIP != "" || svc.Annotations[annotationLoadBalancerIP] != "" {
		t.Fatalf("removed address kept: %q %v", svc.Spec.LoadBalancerIP, svc.Annotations)
	}

	// an address set by others survives
	svc.Spec.LoadBalancerIP = "198.51.100.1"
	if _, err := kube.CoreV1().Services("system").Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	apply()

	if svc := get(); svc.Spec.LoadBalancerIP != "198.51.100.1" {
		t.Fatalf("foreign load balancer ip = %q", svc.Spec.LoadBalancerIP)
	}

	// with an annotation all addresses are requested through it
	p.cfg.Proxy.AddressesAnnotation = "metallb.io/loadBalancerIPs"
	gw.Spec.Addresses = []gwv1.GatewaySpecAddress{address("", "192.0.2.10"), address("", "192.0.2.12")}
	apply()

	if svc := get(); svc.Annotations["metallb.io/loadBalancerIPs"] != "192.0.2.10,192.0.2.12" {
		t.Fatalf("annotations = %v", svc.Annotations)
	}

	gw.Spec.Addresses = nil
	apply()

	if svc := get(); svc.Annotations["metallb.io/loadBalancerIPs"] != "" {
		t.Fatalf("removed annotation kept: %v", svc.Annotations)
	}
}

func TestUnassignedAddresses(t *testing.T) {
	gw := gatewayObj("app", "gw")
	gw.Spec.Addresses = []gwv1.GatewaySpecAddress{address("", "192.0.2.10"), address("", ""), address("", "2001:db8::1")}

	svc := &corev1.Service{Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "2001:0db8::0001"}}}}}

	if got := unassignedAddresses(svc, gw); len(got) != 1 || got[0] != "192.0.2.10" {
		t.Fatalf("unassigned = %v", got)
	}
}

func TestRequestedAddressesNormalized(t *testing.T) {
	gw := gatewayObj("app", "gw")
	gw.Spec.Addresses = []gwv1.GatewaySpecAddress{address("", "2001:db8::1"), address("", "2001:DB8:0::1")}

	if ips, _ := requestedAddresses(gw); len(ips) != 1 || ips[0] != "2001:db8::1" {
		t.Fatalf("ips = %v", ips)
	}
}
