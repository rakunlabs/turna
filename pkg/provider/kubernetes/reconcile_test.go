package kubernetes

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server"
)

func TestProxyNameNamespaceCollision(t *testing.T) {
	if proxyName(nsName{"a-b", "c"}) == proxyName(nsName{"a", "b-c"}) {
		t.Fatal("different gateways share a proxy name")
	}
}

func TestGeneratedNamesKeepIdentity(t *testing.T) {
	for _, pair := range [][2][]string{
		{{"k8s", "a-b", "c"}, {"k8s", "a", "b-c"}},
		{{"k8s", "*.example.com"}, {"k8s", "_.example.com"}},
		{{"k8s-svc", strings.Repeat("x", 250) + "a"}, {"k8s-svc", strings.Repeat("x", 250) + "b"}},
	} {
		a, b := sanitize(pair[0]...), sanitize(pair[1]...)
		if a == b || len(a) > 200 || len(b) > 200 {
			t.Fatalf("generated names collide or are too long: %q, %q", a, b)
		}
	}
}

func TestProxyDeploymentUpdates(t *testing.T) {
	for _, field := range []string{"resources", "labels", "pull-policy", "pull-secrets"} {
		t.Run(field, func(t *testing.T) {
			p := &Provider{kube: kubefake.NewSimpleClientset()}
			p.cfg.Proxy = Proxy{Image: "example:1", CPURequest: "100m", ImagePullPolicy: "IfNotPresent"}
			gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
			ctx := context.Background()
			name := proxyName(key(gw))
			if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "resources":
				p.cfg.Proxy.CPURequest = "500m"
			case "labels":
				gw.Spec.Infrastructure = &gwv1.GatewayInfrastructure{Labels: map[gwv1.LabelKey]gwv1.LabelValue{"team": "platform"}}
			case "pull-policy":
				p.cfg.Proxy.ImagePullPolicy = "Always"
			case "pull-secrets":
				p.cfg.Proxy.ImagePullSecrets = []string{"registry"}
			}
			if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
				t.Fatal(err)
			}
			dep, err := p.kube.AppsV1().Deployments("system").Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			switch field {
			case "resources":
				q := dep.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
				if q.MilliValue() != 500 {
					t.Fatalf("CPU = %s, want 500m", q.String())
				}
			case "labels":
				if dep.Spec.Template.Labels["team"] != "platform" {
					t.Fatal("infrastructure labels not applied")
				}
			case "pull-policy":
				if dep.Spec.Template.Spec.Containers[0].ImagePullPolicy != corev1.PullAlways {
					t.Fatal("pull policy not applied")
				}
			case "pull-secrets":
				if len(dep.Spec.Template.Spec.ImagePullSecrets) != 1 {
					t.Fatal("pull secrets not applied")
				}
			}
			p.kube.(*kubefake.Clientset).ClearActions()
			if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
				t.Fatal(err)
			}
			for _, action := range p.kube.(*kubefake.Clientset).Actions() {
				if action.GetVerb() == "update" {
					t.Fatalf("unchanged %s triggered an update", field)
				}
			}
		})
	}
}

func TestProxyCleanupOwnership(t *testing.T) {
	for _, field := range []string{"controller-name", "installation", "scope"} {
		t.Run(field, func(t *testing.T) {
			kube := kubefake.NewSimpleClientset()
			owner := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Namespace: "system", Image: "example:1"}}}
			gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
			ctx := context.Background()
			name := proxyName(key(gw))
			if err := owner.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
				t.Fatal(err)
			}
			other := &Provider{kube: kube, cfg: owner.cfg}
			switch field {
			case "controller-name":
				other.cfg.ControllerName = "other.example/controller"
			case "installation":
				other.cfg.ControllerID = "other-installation"
			case "scope":
				other.cfg.Namespaces = []string{"other"}
			}
			if err := other.provision(ctx, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := kube.AppsV1().Deployments("system").Get(ctx, name, metav1.GetOptions{}); err != nil {
				t.Fatalf("foreign deployment deleted: %v", err)
			}
			if _, err := kube.CoreV1().Services("system").Get(ctx, name, metav1.GetOptions{}); err != nil {
				t.Fatalf("foreign service deleted: %v", err)
			}
			if _, err := kube.CoreV1().ConfigMaps("system").Get(ctx, name, metav1.GetOptions{}); err != nil {
				t.Fatalf("foreign configmap deleted: %v", err)
			}
			if err := other.applyProxy(ctx, "system", name, gw, nil, nil); err == nil {
				t.Fatal("foreign resources adopted")
			}
			if err := owner.provision(ctx, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := kube.AppsV1().Deployments("system").Get(ctx, name, metav1.GetOptions{}); err == nil {
				t.Fatal("owned deployment not cleaned up")
			}
		})
	}
}

func TestProxyCleanupRetriesOrphans(t *testing.T) {
	kube := kubefake.NewSimpleClientset()
	p := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Namespace: "system", Image: "example:1"}}}
	gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
	ctx := context.Background()
	name := proxyName(key(gw))
	if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
		t.Fatal(err)
	}
	var deletes int
	kube.PrependReactor("delete", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		deletes++
		if deletes == 1 {
			return true, nil, fmt.Errorf("temporary API error")
		}
		return false, nil, nil
	})
	if err := p.provision(ctx, nil, nil, nil); err == nil {
		t.Fatal("deletion error was swallowed")
	}
	if err := p.provision(ctx, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.CoreV1().Services("system").Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Fatal("orphaned service not retried")
	}
}

func TestProxyDeploymentIgnoresAPIDefaults(t *testing.T) {
	kube := kubefake.NewSimpleClientset()
	p := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Image: "example:1"}}}
	ctx := context.Background()
	gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
	name := proxyName(key(gw))
	if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
		t.Fatal(err)
	}
	dep, err := kube.AppsV1().Deployments("system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec := &dep.Spec.Template.Spec
	spec.DNSPolicy = corev1.DNSClusterFirst
	spec.RestartPolicy = corev1.RestartPolicyAlways
	spec.TerminationGracePeriodSeconds = ptr(int64(30))
	spec.SchedulerName = corev1.DefaultSchedulerName
	c := &spec.Containers[0]
	c.TerminationMessagePath = corev1.TerminationMessagePathDefault
	c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	for _, probe := range []*corev1.Probe{c.ReadinessProbe, c.LivenessProbe} {
		probe.TimeoutSeconds = 1
		probe.SuccessThreshold = 1
		probe.FailureThreshold = 3
		probe.HTTPGet.Scheme = corev1.URISchemeHTTP
	}
	if _, err := kube.AppsV1().Deployments("system").Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	kube.ClearActions()
	if err := p.applyProxy(ctx, "system", name, gw, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, action := range kube.Actions() {
		if action.GetVerb() == "update" {
			t.Fatalf("API defaults triggered update: %s", action.GetResource().Resource)
		}
	}
}

func TestHostnameRouterNames(t *testing.T) {
	snap := &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("demo", "web", httpListener("http", 80, ""))},
		HTTPRoutes: []*gwv1.HTTPRoute{
			routeObj("demo", "wild", "web", []string{"*.example.com"}, gwv1.HTTPRouteRule{}),
			routeObj("demo", "exact", "web", []string{"wildcard.example.com"}, gwv1.HTTPRouteRule{}),
		},
	}
	first := Translate(&Config{}, snap).Server
	if len(first.HTTP.Routers) != 2 {
		t.Fatalf("routers = %v", first.HTTP.Routers)
	}
	for range 20 {
		if next := Translate(&Config{}, snap).Server; !reflect.DeepEqual(first, next) {
			t.Fatal("translation is nondeterministic")
		}
	}
}

type retryApplier struct{ calls atomic.Int64 }

func (a *retryApplier) SetLayer(string, *server.Server) error {
	if a.calls.Add(1) == 1 {
		return fmt.Errorf("temporary apply error")
	}
	return nil
}

// A snapshot with no informer events isolates retries from event-driven syncs.
func snapshotListers(s *Snapshot) listers {
	return listers{
		classes:   func() ([]*gwv1.GatewayClass, error) { return s.GatewayClasses, nil },
		gateways:  func() ([]*gwv1.Gateway, error) { return s.Gateways, nil },
		routes:    func() ([]*gwv1.HTTPRoute, error) { return s.HTTPRoutes, nil },
		grpc:      func() ([]*gwv1.GRPCRoute, error) { return nil, nil },
		tls:       func() ([]*gwv1.TLSRoute, error) { return nil, nil },
		tcp:       func() ([]*gwv1.TCPRoute, error) { return nil, nil },
		udp:       func() ([]*gwv1.UDPRoute, error) { return nil, nil },
		grants:    func() ([]*gwv1.ReferenceGrant, error) { return nil, nil },
		services:  func() ([]*corev1.Service, error) { return s.Services, nil },
		slices:    func() ([]*discoveryv1.EndpointSlice, error) { return s.EndpointSlices, nil },
		secrets:   func() ([]*corev1.Secret, error) { return nil, nil },
		namespace: func() ([]*corev1.Namespace, error) { return nil, nil },
	}
}

func TestProviderRetriesApplyWithoutEvents(t *testing.T) {
	a := &retryApplier{}
	p := NewWithClients(Config{Debounce: time.Millisecond}, a, nil, nil)
	p.listers = snapshotListers(&Snapshot{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); p.loop(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	p.notify()
	select {
	case <-p.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("failed apply was not retried")
	}
	if a.calls.Load() != 2 {
		t.Fatalf("apply calls = %d", a.calls.Load())
	}
}

func TestControllerOnlyLeaderProvisions(t *testing.T) {
	p := NewWithClients(Config{Mode: ModeController, Proxy: Proxy{Namespace: "system", Image: "example:1"}}, nil, kubefake.NewSimpleClientset(), nil)
	p.listers = snapshotListers(&Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{gatewayObj("demo", "web", httpListener("http", 80, ""))}})
	p.status = newStatusWriter(p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.sync(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.kube.(*kubefake.Clientset).Actions()) != 0 {
		t.Fatal("standby mutated proxy resources")
	}
	p.status.setLeader(ctx)
	if err := p.sync(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.kube.(*kubefake.Clientset).Actions()) == 0 {
		t.Fatal("leader did not provision")
	}
	p.kube.(*kubefake.Clientset).ClearActions()
	cancel()
	if err := p.sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(p.kube.(*kubefake.Clientset).Actions()) != 0 {
		t.Fatal("expired leader mutated proxy resources")
	}
}

func TestControllerRetriesProvisionWithoutEvents(t *testing.T) {
	kube := kubefake.NewSimpleClientset()
	var creates atomic.Int64
	kube.PrependReactor("create", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		if creates.Add(1) == 1 {
			return true, nil, fmt.Errorf("temporary create error")
		}
		return false, nil, nil
	})
	p := NewWithClients(Config{Mode: ModeController, Debounce: time.Millisecond, Status: Status{Disabled: true}, Proxy: Proxy{Namespace: "system", Image: "example:1"}}, nil, kube, nil)
	p.listers = snapshotListers(&Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{gatewayObj("demo", "web", httpListener("http", 80, ""))}})
	p.status = newStatusWriter(p)
	ctx, cancel := context.WithCancel(context.Background())
	p.status.setLeader(ctx)
	done := make(chan struct{})
	go func() { defer close(done); p.loop(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-p.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("failed provisioning was not retried")
	}
	if creates.Load() != 2 {
		t.Fatalf("ConfigMap create calls = %d", creates.Load())
	}
}

func TestMirrorGroupsServiceEndpoints(t *testing.T) {
	svc, slice := serviceObj("demo", "shadow", 80)
	slice.Endpoints = []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}, {Addresses: []string{"10.0.0.2"}}}
	tr := &translator{cfg: &Config{}, idx: newIndex(&Snapshot{Services: []*corev1.Service{svc}, EndpointSlices: []*discoveryv1.EndpointSlice{slice}})}
	m, err := tr.mirror(&gwv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: "demo"}}, &gwv1.HTTPRequestMirrorFilter{BackendRef: gwv1.BackendObjectReference{Name: "shadow", Port: ptr(gwv1.PortNumber(80))}})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Servers) != 1 || len(m.Servers[0].Endpoints) != 2 || *m.Servers[0].Percent != 100 {
		t.Fatalf("mirror = %+v", m)
	}
}

func TestProvisionErrorIsPerGateway(t *testing.T) {
	kube := kubefake.NewSimpleClientset()
	p := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Namespace: "system", Image: "example:1"}}}
	good := gatewayObj("demo", "good", httpListener("http", 80, ""))
	bad := gatewayObj("demo", "bad", httpListener("http", 80, ""))
	badName := proxyName(key(bad))
	kube.PrependReactor("create", "configmaps", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.(k8stesting.CreateAction).GetObject().(metav1.Object).GetName() == badName {
			return true, nil, fmt.Errorf("quota exceeded")
		}
		return false, nil, nil
	})
	err := p.provision(context.Background(), []*gwv1.Gateway{good, bad}, nil, nil)
	if err == nil {
		t.Fatal("provision error was swallowed")
	}
	if gatewayError(err, key(good)) != nil {
		t.Errorf("healthy gateway got error: %v", gatewayError(err, key(good)))
	}
	if gatewayError(err, key(bad)) == nil {
		t.Error("failing gateway has no error")
	}
	if other := fmt.Errorf("apply failed"); gatewayError(other, key(good)) != other {
		t.Error("non-provision errors must apply to every gateway")
	}
}

func TestProxyNameIsDNSLabel(t *testing.T) {
	for _, gw := range []nsName{{"app", "my.gw"}, {"app", "web"}, {strings.Repeat("n", 63), strings.Repeat("g.", 100) + "x"}} {
		name := proxyName(gw)
		if errs := validation.IsDNS1035Label(name); len(errs) > 0 {
			t.Errorf("proxyName(%s) = %q: %v", gw, name, errs)
		}
	}
	if proxyName(nsName{"app", "a.b"}) == proxyName(nsName{"app", "a-b"}) {
		t.Fatal("dotted and dashed gateway names share a proxy name")
	}
}

func TestListenerPortOutOfRangeWithOffset(t *testing.T) {
	gw := gatewayObj("app", "gw", httpListener("ok", 80, ""), httpListener("high", 60000, ""))
	res := Translate(&Config{PortOffset: 8000}, &Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{gw}})
	for _, l := range res.Gateways[key(gw)].Listeners {
		accepted := meta.FindStatusCondition(l.Conditions, string(gwv1.ListenerConditionAccepted))
		if want := l.Name == "ok"; (accepted.Status == metav1.ConditionTrue) != want {
			t.Errorf("listener %s accepted = %s (%s)", l.Name, accepted.Status, accepted.Message)
		}
	}
	if _, ok := res.Server.EntryPoints["k8s-60000"]; ok {
		t.Error("out of range listener got an entrypoint")
	}
	if ports := listenerPorts(gw, 8000, 9901); len(ports) != 1 || ports[0].port != 80 {
		t.Errorf("proxy ports = %+v", ports)
	}
}
