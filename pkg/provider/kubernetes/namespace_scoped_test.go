package kubernetes

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"

	"github.com/rakunlabs/turna/pkg/server"
)

func TestNamespaceScopedValidate(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg Config
		ok  bool
	}{
		"cluster":       {Config{}, true},
		"scoped":        {Config{NamespaceScoped: true, Namespaces: []string{"team"}, GatewayClass: "team"}, true},
		"no namespaces": {Config{NamespaceScoped: true, GatewayClass: "team"}, false},
		"all":           {Config{NamespaceScoped: true, Namespaces: []string{""}, GatewayClass: "team"}, false},
		"no class":      {Config{NamespaceScoped: true, Namespaces: []string{"team"}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if err := tc.cfg.validate(); (err == nil) != tc.ok {
				t.Fatalf("validate = %v, want ok %v", err, tc.ok)
			}
		})
	}
}

// Installations sharing a controller name serve only their own class.
func TestTranslateGatewayClassFilter(t *testing.T) {
	other := class()
	other.Name = "other"

	gw := gatewayObj("app", "gw", httpListener("http", 80, ""))
	foreign := gatewayObj("app", "foreign", httpListener("http", 81, ""))
	foreign.Spec.GatewayClassName = "other"

	res := Translate(&Config{GatewayClass: "turna"}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class(), other},
		Gateways:       []*gwv1.Gateway{gw, foreign},
	})

	if _, ok := res.GatewayClasses["other"]; ok {
		t.Fatal("foreign class got status")
	}
	if _, ok := res.Gateways[key(foreign)]; ok {
		t.Fatal("gateway of a foreign class translated")
	}
	if _, ok := res.Gateways[key(gw)]; !ok {
		t.Fatal("gateway of the configured class not translated")
	}
}

func TestProxyConfigScope(t *testing.T) {
	plain, err := (&Config{}).proxyConfig(nsName{Namespace: "a", Name: "b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "gateway_class") || strings.Contains(string(plain), "namespace_scoped") {
		t.Fatalf("unset scope fields change existing proxy configs:\n%s", plain)
	}

	scoped, err := (&Config{NamespaceScoped: true, Namespaces: []string{"team"}, GatewayClass: "team"}).proxyConfig(nsName{Namespace: "team", Name: "b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scoped), "gateway_class: team") || !strings.Contains(string(scoped), "namespace_scoped: true") {
		t.Fatalf("proxy does not inherit the scope:\n%s", scoped)
	}
}

// A namespace scoped provider must work with namespaced permissions plus a
// read of its GatewayClass.
func TestProviderNamespaceScoped(t *testing.T) {
	local := freePort(t)

	kube := kubefake.NewSimpleClientset()
	gw := gwfake.NewSimpleClientset()
	bg := context.Background()

	team := class()
	team.Name = "team"
	if _, err := gw.GatewayV1().GatewayClasses().Create(bg, team, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	g := gatewayObj("team", "gw", httpListener("http", 80, ""))
	g.Spec.GatewayClassName = "team"
	if _, err := gw.GatewayV1().Gateways("team").Create(bg, g, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	var (
		mu         sync.Mutex
		violations []string
	)
	deny := func(a k8stesting.Action) (bool, runtime.Object, error) {
		mu.Lock()
		defer mu.Unlock()
		violations = append(violations, a.GetVerb()+" "+a.GetResource().Resource+" ns="+a.GetNamespace())
		return false, nil, nil
	}
	watchDeny := func(a k8stesting.Action) (bool, watch.Interface, error) {
		_, _, err := deny(a)
		return false, nil, err
	}

	kube.PrependReactor("*", "namespaces", deny)
	kube.PrependWatchReactor("namespaces", watchDeny)
	gw.PrependReactor("update", "gatewayclasses", deny)
	for _, c := range []*k8stesting.Fake{&kube.Fake, &gw.Fake} {
		c.PrependReactor("list", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
			if a.GetResource().Resource != "gatewayclasses" && a.GetNamespace() != "team" {
				return deny(a)
			}
			if a.GetResource().Resource == "gatewayclasses" {
				f := a.(k8stesting.ListAction).GetListRestrictions().Fields
				if v, ok := f.RequiresExactMatch("metadata.name"); !ok || v != "team" {
					return deny(a)
				}
			}
			return false, nil, nil
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	manager := server.NewManager(ctx, wg)

	t.Cleanup(func() {
		cancel()
		manager.Shutdown()
		wg.Wait()
	})

	t.Setenv("POD_NAMESPACE", "team")

	p := NewWithClients(Config{
		NamespaceScoped: true,
		GatewayClass:    "team",
		Address:         "127.0.0.1",
		PortOffset:      local - 80,
		Debounce:        10 * time.Millisecond,
		Status:          Status{LeaderElection: LeaderElection{Disabled: true}, Addresses: []string{"192.0.2.1"}},
	}, manager, kube, gw)

	go func() {
		if err := p.Run(ctx); err != nil {
			t.Errorf("run: %v", err)
		}
	}()

	select {
	case <-p.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("provider not ready")
	}

	eventually(t, "gateway status", func() bool {
		cur, err := gw.GatewayV1().Gateways("team").Get(ctx, "gw", metav1.GetOptions{})
		return err == nil && meta.IsStatusConditionTrue(cur.Status.Conditions, string(gwv1.GatewayConditionProgrammed))
	})

	mu.Lock()
	defer mu.Unlock()
	if len(violations) > 0 {
		t.Fatalf("requests outside the namespace scope: %v", violations)
	}
}
