package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func listenUnix(t *testing.T, h http.Handler) string {
	t.Helper()

	// t.TempDir can exceed the socket path limit on macOS
	dir, err := os.MkdirTemp("", "svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socket := filepath.Join(dir, "up.sock")

	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}

	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })

	return socket
}

func TestUnixUpstream(t *testing.T) {
	health := make(chan struct{}, 1)
	socket := listenUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			select {
			case health <- struct{}{}:
			default:
			}
		}

		_, _ = io.WriteString(w, r.Method+" "+r.URL.RequestURI())
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := newServiceHandler(t, ctx, &Service{
		// an unreachable proxy proves unix upstreams bypass it
		Proxy: "http://127.0.0.1:1",
		LoadBalancer: LoadBalancer{
			Servers:     []Server{{URL: "unix://" + socket}},
			HealthCheck: &HealthCheck{Path: "/health", Interval: time.Hour},
		},
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1.47/containers/json?all=1", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "GET /v1.47/containers/json?all=1" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	select {
	case <-health:
	case <-time.After(5 * time.Second):
		t.Fatal("health check did not reach the socket")
	}
}

func TestUnixMirror(t *testing.T) {
	mirrored := make(chan string, 1)
	socket := listenUnix(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mirrored <- r.URL.Path
	}))

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer upstream.Close()

	h := newServiceHandler(t, context.Background(), &Service{
		LoadBalancer: LoadBalancer{Servers: []Server{{URL: upstream.URL}}},
		Mirror:       &Mirror{Servers: []MirrorServer{{URL: "unix://" + socket}}},
	})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/shadow", nil))

	select {
	case p := <-mirrored:
		if p != "/shadow" {
			t.Fatalf("mirrored path = %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mirror did not reach the socket")
	}
}

func TestUnixURLInvalid(t *testing.T) {
	for _, raw := range []string{"unix://host/x.sock", "unix://", "unix:///x.sock?a=1"} {
		_, err := (&Service{LoadBalancer: LoadBalancer{Servers: []Server{{URL: raw}}}}).Middleware(context.Background())
		if err == nil {
			t.Fatalf("%s: expected error", raw)
		}
	}
}
