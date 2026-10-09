package kubernetes

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"

	"github.com/rakunlabs/turna/pkg/server"
)

type countingApplier struct {
	m *server.Manager
	n atomic.Int64
}

func (c *countingApplier) SetLayer(name string, s *server.Server) error {
	c.n.Add(1)

	return c.m.SetLayer(name, s)
}

// TestProviderAppliesOnlyChanges checks that status updates and unrelated
// events do not re-apply an unchanged configuration.
func TestProviderAppliesOnlyChanges(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer backend.Close()

	host, portStr, _ := net.SplitHostPort(backend.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	svc, slice := serviceObj("app", "web", 80)
	slice.Ports[0].Port = ptr(int32(port))
	slice.Endpoints = []discoveryv1.Endpoint{{Addresses: []string{host}, Conditions: discoveryv1.EndpointConditions{Ready: ptr(true)}}}

	kube := kubefake.NewSimpleClientset(svc, slice, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}})
	gw := gwfake.NewSimpleClientset()
	bg := context.Background()

	_, _ = gw.GatewayV1().GatewayClasses().Create(bg, class(), metav1.CreateOptions{})
	_, _ = gw.GatewayV1().Gateways("app").Create(bg, gatewayObj("app", "gw", httpListener("http", 80, "")), metav1.CreateOptions{})
	_, _ = gw.GatewayV1().HTTPRoutes("app").Create(bg, routeObj("app", "r", "gw", nil, backendRule("/", "web", 80)), metav1.CreateOptions{})

	ctx, cancel := context.WithCancel(bg)
	wg := &sync.WaitGroup{}
	manager := server.NewManager(ctx, wg)

	t.Cleanup(func() {
		cancel()
		manager.Shutdown()
		wg.Wait()
	})

	applier := &countingApplier{m: manager}
	local := freePort(t)

	p := NewWithClients(Config{
		Address: "127.0.0.1", PortOffset: local - 80, Debounce: 5 * time.Millisecond,
		Status: Status{LeaderElection: LeaderElection{Disabled: true}},
	}, applier, kube, gw)

	go func() { _ = p.Run(ctx) }()

	<-p.Ready()

	// status writes and an unrelated secret change trigger syncs
	time.Sleep(300 * time.Millisecond)
	_, _ = kube.CoreV1().Secrets("app").Create(bg, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "x"}}, metav1.CreateOptions{})
	time.Sleep(300 * time.Millisecond)

	if n := applier.n.Load(); n != 1 {
		t.Fatalf("configuration applied %d times, want 1", n)
	}
}
