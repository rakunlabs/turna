package kubernetes

import (
	"context"
	"io"
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
	"github.com/rakunlabs/turna/pkg/server/registry"
)

// TestProviderNoErrorsDuringReload sends requests while endpoints change and
// expects every request to succeed.
func TestProviderNoErrorsDuringReload(t *testing.T) {
	prev := registry.DrainTimeout
	registry.DrainTimeout = time.Second
	t.Cleanup(func() { registry.DrainTimeout = prev })

	var backends []string
	for range 4 {
		b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		t.Cleanup(b.Close)
		backends = append(backends, b.Listener.Addr().String())
	}

	endpoints := func(addrs ...string) ([]discoveryv1.Endpoint, int32) {
		var out []discoveryv1.Endpoint
		var port int32

		for _, a := range addrs {
			host, p, _ := net.SplitHostPort(a)
			n, _ := strconv.Atoi(p)
			port = int32(n)
			out = append(out, discoveryv1.Endpoint{Addresses: []string{host}, Conditions: discoveryv1.EndpointConditions{Ready: ptr(true)}})
		}

		return out, port
	}

	// one slice per backend port, httptest servers listen on different ports
	svc, _ := serviceObj("app", "web", 80)
	var slices []*discoveryv1.EndpointSlice

	for i, b := range backends {
		eps, port := endpoints(b)
		slices = append(slices, &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "app", Name: "web-" + strconv.Itoa(i),
				Labels: map[string]string{discoveryv1.LabelServiceName: "web"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports:       []discoveryv1.EndpointPort{{Name: ptr("http"), Port: ptr(port)}},
			Endpoints:   eps,
		})
	}

	kube := kubefake.NewSimpleClientset(svc, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}})
	bg := context.Background()

	for _, s := range slices[:2] {
		if _, err := kube.DiscoveryV1().EndpointSlices("app").Create(bg, s, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	gw := gwfake.NewSimpleClientset()
	_, _ = gw.GatewayV1().GatewayClasses().Create(bg, class(), metav1.CreateOptions{})
	_, _ = gw.GatewayV1().Gateways("app").Create(bg, gatewayObj("app", "gw", httpListener("http", 80, "")), metav1.CreateOptions{})
	_, _ = gw.GatewayV1().HTTPRoutes("app").Create(bg, routeObj("app", "r", "gw", nil, backendRule("/", "web", 80)), metav1.CreateOptions{})

	local := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	manager := server.NewManager(ctx, wg)

	t.Cleanup(func() {
		cancel()
		manager.Shutdown()
		wg.Wait()
	})

	p := NewWithClients(Config{
		Address: "127.0.0.1", PortOffset: local - 80, Debounce: 5 * time.Millisecond,
		Status: Status{Disabled: true},
	}, manager, kube, gw)

	go func() { _ = p.Run(ctx) }()

	select {
	case <-p.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("provider not ready")
	}

	var (
		failures atomic.Int64
		total    atomic.Int64
		stop     = make(chan struct{})
		clients  sync.WaitGroup
	)

	client := &http.Client{Timeout: 5 * time.Second}

	for range 8 {
		clients.Add(1)

		go func() {
			defer clients.Done()

			for {
				select {
				case <-stop:
					return
				default:
				}

				res, err := client.Get("http://127.0.0.1:" + strconv.Itoa(local) + "/")
				total.Add(1)

				if err != nil {
					failures.Add(1)
					t.Logf("request error: %v", err)

					continue
				}

				b, _ := io.ReadAll(res.Body)
				_ = res.Body.Close()

				if res.StatusCode != http.StatusOK {
					failures.Add(1)
					t.Logf("status %d: %s", res.StatusCode, b)
				}
			}
		}()
	}

	// add and remove endpoints while traffic flows; the old endpoints stay
	// alive like terminating pods do
	for i := range 20 {
		s := slices[2+i%2]
		if i%4 < 2 {
			_, _ = kube.DiscoveryV1().EndpointSlices("app").Create(bg, s, metav1.CreateOptions{})
		} else {
			_ = kube.DiscoveryV1().EndpointSlices("app").Delete(bg, s.Name, metav1.DeleteOptions{})
		}

		time.Sleep(30 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	clients.Wait()

	if failures.Load() > 0 {
		t.Fatalf("%d of %d requests failed during reloads", failures.Load(), total.Load())
	}

	t.Logf("%d requests without errors", total.Load())
}
