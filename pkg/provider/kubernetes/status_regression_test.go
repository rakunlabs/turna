package kubernetes

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

func TestStatusSkipsStaleSnapshots(t *testing.T) {
	for _, change := range []string{"generation", "uid", "unchanged"} {
		t.Run(change, func(t *testing.T) {
			gc := class()
			gw := gatewayObj("app", "gw", httpListener("http", 80, ""))
			http := routeObj("app", "http", "gw", nil, gwv1.HTTPRouteRule{})
			grpc := &gwv1.GRPCRoute{ObjectMeta: objMeta("app", "grpc")}
			tls := &gwv1.TLSRoute{ObjectMeta: objMeta("app", "tls")}
			tcp := &gwv1.TCPRoute{ObjectMeta: objMeta("app", "tcp")}
			udp := &gwv1.UDPRoute{ObjectMeta: objMeta("app", "udp")}
			snap := &Snapshot{GatewayClasses: []*gwv1.GatewayClass{gc}, Gateways: []*gwv1.Gateway{gw}, HTTPRoutes: []*gwv1.HTTPRoute{http}, GRPCRoutes: []*gwv1.GRPCRoute{grpc}, TLSRoutes: []*gwv1.TLSRoute{tls}, TCPRoutes: []*gwv1.TCPRoute{tcp}, UDPRoutes: []*gwv1.UDPRoute{udp}}
			sources := []runtime.Object{gc, gw, http, grpc, tls, tcp, udp}
			current := make([]runtime.Object, 0, len(sources))
			for _, source := range sources {
				source.(metav1.Object).SetUID("original")
				obj := source.DeepCopyObject()
				switch change {
				case "generation":
					obj.(metav1.Object).SetGeneration(2)
				case "uid":
					obj.(metav1.Object).SetUID("recreated")
				}
				current = append(current, obj)
			}
			client := gwfake.NewSimpleClientset(current...)
			// Seed through the typed API: the fake tracker's plural inference
			// does not map Gateway to the generated client's resource name.
			if _, err := client.GatewayV1().Gateways("app").Create(context.Background(), current[1].(*gwv1.Gateway), metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			p := NewWithClients(Config{}, nopApplier{}, kubefake.NewSimpleClientset(), client)
			w := newStatusWriter(p)
			res := Translate(&Config{}, snap)
			for i, kind := range []gwv1.Kind{kindRoute, kindGRPCRoute, kindTLSRoute, kindTCPRoute, kindUDPRoute} {
				obj := sources[i+2].(metav1.Object)
				res.Routes[routeKey{Kind: kind, nsName: key(obj)}] = []gwv1.RouteParentStatus{{
					ParentRef: gwv1.ParentReference{Name: "gw"}, ControllerName: DefaultControllerName,
					Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, Reason: "Accepted", ObservedGeneration: 1}},
				}}
			}
			w.write(context.Background(), &statusUpdate{snap: snap, res: res})
			updates := 0
			for _, action := range client.Actions() {
				if action.GetVerb() == "update" && action.GetSubresource() == "status" {
					updates++
					if change != "unchanged" {
						t.Errorf("stale snapshot updated %s status", action.GetResource().Resource)
					}
				}
			}
			if change == "unchanged" && updates != len(sources) {
				t.Errorf("current snapshot wrote %d statuses, want %d", updates, len(sources))
			}
		})
	}
}

func TestProxyStatusRejectsForeignResourcesAndUnobservedRollouts(t *testing.T) {
	for _, state := range []string{"ready", "unobserved", "foreign-service", "foreign-deployment"} {
		t.Run(state, func(t *testing.T) {
			p := &Provider{cfg: Config{Proxy: Proxy{Namespace: "system"}}}
			gw := nsName{Namespace: "app", Name: "gw"}
			lbls := proxyLabels(gw)
			lbls[labelController] = p.controllerID()
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "system", Name: proxyName(gw), Labels: lbls}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.10"}}
			dep := &appsv1.Deployment{ObjectMeta: *svc.ObjectMeta.DeepCopy(), Status: appsv1.DeploymentStatus{ReadyReplicas: 1, ObservedGeneration: 2}}
			dep.Generation = 2
			switch state {
			case "unobserved":
				dep.Status.ObservedGeneration = 1
			case "foreign-service":
				svc.Labels = map[string]string{labelManagedBy: managedByValue, labelController: "other-installation"}
			case "foreign-deployment":
				dep.Labels[labelController] = "other-installation"
			}
			p.kube = kubefake.NewSimpleClientset(svc, dep)
			addresses, ready := newStatusWriter(p).proxyStatus(context.Background(), gw)
			if ready != (state == "ready") {
				t.Errorf("ready = %v for %s", ready, state)
			}
			if state == "ready" && len(addresses) != 1 {
				t.Fatal("owned service address not published")
			}
			if (state == "foreign-service" || state == "foreign-deployment") && len(addresses) != 0 {
				t.Fatal("foreign proxy address published")
			}
		})
	}
}
