package kubernetes

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"

	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	return l.Addr().(*net.TCPAddr).Port
}

func eventually(t *testing.T, msg string, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", msg)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func TestProviderEndToEnd(t *testing.T) {
	prev := registry.DrainTimeout
	registry.DrainTimeout = 0
	t.Cleanup(func() { registry.DrainTimeout = prev })

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "backend %s %s", r.Host, r.URL.Path)
	}))
	defer backend.Close()

	backendHost, backendPortStr, _ := net.SplitHostPort(backend.Listener.Addr().String())
	backendPort, _ := strconv.Atoi(backendPortStr)

	svc, slice := serviceObj("app", "web", 80)
	slice.Ports[0].Port = ptr(int32(backendPort))
	slice.Endpoints = []discoveryv1.Endpoint{{Addresses: []string{backendHost}, Conditions: discoveryv1.EndpointConditions{Ready: ptr(true)}}}

	// listener port 80 is mapped with an offset to a free local port
	local := freePort(t)

	kube := kubefake.NewSimpleClientset(svc, slice, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}})
	// NewClientset's managed field tracker cannot map the Gateway kind
	gw := gwfake.NewSimpleClientset()
	bg := context.Background()

	if _, err := gw.GatewayV1().GatewayClasses().Create(bg, class(), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.GatewayV1().Gateways("app").Create(bg, gatewayObj("app", "gw", httpListener("http", 80, "")), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.GatewayV1().HTTPRoutes("app").Create(bg, routeObj("app", "r", "gw", []string{"example.com"}, backendRule("/", "web", 80)), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	manager := server.NewManager(ctx, wg)

	t.Cleanup(func() {
		cancel()
		manager.Shutdown()
		wg.Wait()
	})

	cfg := Config{
		Address:    "127.0.0.1",
		PortOffset: local - 80,
		Debounce:   10 * time.Millisecond,
		Status:     Status{LeaderElection: LeaderElection{Disabled: true}, Addresses: []string{"192.0.2.1"}},
	}

	p := NewWithClients(cfg, manager, kube, gw)

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

	get := func(host, path string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(local)+path, nil)
		req.Host = host

		res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer func() { _ = res.Body.Close() }()

		b, _ := io.ReadAll(res.Body)

		return res.StatusCode, string(b)
	}

	if code, body := get("example.com", "/hello"); code != 200 || body != "backend example.com /hello" {
		t.Fatalf("got %d %q", code, body)
	}

	if code, _ := get("other.com", "/hello"); code != 404 {
		t.Fatalf("unknown host = %d", code)
	}

	// status is written
	eventually(t, "route status", func() bool {
		r, err := gw.GatewayV1().HTTPRoutes("app").Get(ctx, "r", metav1.GetOptions{})
		if err != nil || len(r.Status.Parents) != 1 {
			return false
		}

		return meta.IsStatusConditionTrue(r.Status.Parents[0].Conditions, string(gwv1.RouteConditionAccepted))
	})

	eventually(t, "gateway status", func() bool {
		g, err := gw.GatewayV1().Gateways("app").Get(ctx, "gw", metav1.GetOptions{})
		if err != nil {
			return false
		}

		return meta.IsStatusConditionTrue(g.Status.Conditions, string(gwv1.GatewayConditionProgrammed)) &&
			len(g.Status.Addresses) == 1 && g.Status.Addresses[0].Value == "192.0.2.1" &&
			len(g.Status.Listeners) == 1 && g.Status.Listeners[0].AttachedRoutes == 1
	})

	eventually(t, "gatewayclass status", func() bool {
		c, err := gw.GatewayV1().GatewayClasses().Get(ctx, "turna", metav1.GetOptions{})

		return err == nil && meta.IsStatusConditionTrue(c.Status.Conditions, string(gwv1.GatewayClassConditionStatusAccepted)) &&
			len(c.Status.SupportedFeatures) > 0
	})

	// updating the route applies without restart
	route, _ := gw.GatewayV1().HTTPRoutes("app").Get(ctx, "r", metav1.GetOptions{})
	route.Spec.Rules[0].Filters = []gwv1.HTTPRouteFilter{{
		Type: gwv1.HTTPRouteFilterURLRewrite,
		URLRewrite: &gwv1.HTTPURLRewriteFilter{Path: &gwv1.HTTPPathModifier{
			Type: gwv1.PrefixMatchHTTPPathModifier, ReplacePrefixMatch: ptr("/v2/"),
		}},
	}}
	route.Generation++

	if _, err := gw.GatewayV1().HTTPRoutes("app").Update(ctx, route, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "rewrite applied", func() bool {
		_, body := get("example.com", "/hello")

		return body == "backend example.com /v2/hello"
	})

	// removing endpoints answers 503
	slice.Endpoints = nil
	if _, err := kube.DiscoveryV1().EndpointSlices("app").Update(ctx, slice, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "no endpoints", func() bool {
		code, _ := get("example.com", "/hello")

		return code == http.StatusServiceUnavailable
	})

	// deleting the gateway closes the entrypoint
	if err := gw.GatewayV1().Gateways("app").Delete(ctx, "gw", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "entrypoint closed", func() bool {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(local), 200*time.Millisecond)
		if err != nil {
			return true
		}

		_ = c.Close()

		return false
	})
}
