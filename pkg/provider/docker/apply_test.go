package docker

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

func newManager(t *testing.T) *server.Manager {
	t.Helper()

	prev := registry.DrainTimeout
	registry.DrainTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	m := server.NewManager(ctx, wg)

	t.Cleanup(func() {
		m.Shutdown()
		cancel()
		wg.Wait()
		registry.DrainTimeout = prev
	})

	return m
}

func backend(t *testing.T, body string) (string, int) {
	t.Helper()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)

	host, port, _ := net.SplitHostPort(s.Listener.Addr().String())
	p, _ := net.LookupPort("tcp", port)

	return host, p
}

func get(t *testing.T, addr, host string) (int, string) {
	t.Helper()

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	req.Host = host

	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}

	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	b, _ := io.ReadAll(res.Body)

	return res.StatusCode, string(b)
}

func TestProviderApply(t *testing.T) {
	m := newManager(t)

	if err := m.SetLayer(server.LayerStatic, &server.Server{
		EntryPoints: map[string]server.EntryPoint{"web": {Address: "127.0.0.1:0"}},
	}); err != nil {
		t.Fatal(err)
	}

	addr, _ := m.Addr("web")

	ipA, portA := backend(t, "a")
	ipB, portB := backend(t, "b")

	p := &Provider{applier: m, reported: map[string]string{}}

	groups := []*group{
		{
			Name: "a", Targets: []string{ipA}, Ports: map[string][]int{"tcp": {portA}},
			Labels: map[string]string{"turna.http.routers.r.host": "a.test"},
		},
		{
			Name: "b", Targets: []string{ipB}, Ports: map[string][]int{"tcp": {portB}},
			Labels: map[string]string{"turna.http.routers.r.host": "b.test"},
		},
		{
			// an invalid strategy fails only when the server builds it
			Name: "broken", Targets: []string{ipB}, Ports: map[string][]int{"tcp": {portB}},
			Labels: map[string]string{
				"turna.http.routers.r.host":          "broken.test",
				"turna.http.services.s.strategy":     "nope",
				"turna.http.routers.r.service":       "s",
				"turna.http.routers.r.path":          "/*",
				"turna.http.routers.r.entrypoints":   "web",
				"turna.http.services.s.health_check": "",
			},
		},
	}

	_, applyErrs := p.apply(groups, m.Current())
	if _, ok := applyErrs["broken"]; !ok || len(applyErrs) != 1 {
		t.Fatalf("apply errors %v", applyErrs)
	}

	if code, body := get(t, addr.String(), "a.test"); code != 200 || body != "a" {
		t.Fatalf("a.test: %d %q", code, body)
	}

	if code, body := get(t, addr.String(), "b.test"); code != 200 || body != "b" {
		t.Fatalf("b.test: %d %q", code, body)
	}

	if code, _ := get(t, addr.String(), "broken.test"); code != 404 {
		t.Fatalf("broken.test: %d", code)
	}

	// a removed group is removed from the server
	if _, errs := p.apply(groups[:1], m.Current()); len(errs) > 0 {
		t.Fatal(errs)
	}

	if code, _ := get(t, addr.String(), "b.test"); code != 404 {
		t.Fatalf("b.test after removal: %d", code)
	}

	// the static layer is never affected
	if m.Layer(server.LayerStatic) == nil {
		t.Fatal("static layer removed")
	}
}
