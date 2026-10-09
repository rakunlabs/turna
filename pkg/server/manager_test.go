package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/hello"
	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/redirect"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	prev := registry.DrainTimeout
	registry.DrainTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	m := NewManager(ctx, wg)

	t.Cleanup(func() {
		m.Shutdown()
		cancel()
		wg.Wait()
		registry.DrainTimeout = prev
	})

	return m
}

func helloServer(entrypoints map[string]EntryPoint, message string) *Server {
	routers := make(map[string]shttp.Router, len(entrypoints))
	for name := range entrypoints {
		routers[name] = shttp.Router{Path: []string{"/*"}, Middlewares: []string{"mgr-hello"}, EntryPoints: []string{name}}
	}

	return &Server{
		EntryPoints: entrypoints,
		HTTP: shttp.HTTP{
			Routers:     routers,
			Middlewares: map[string]shttp.HTTPMiddleware{"mgr-hello": {HelloMiddleware: &hello.Hello{Message: message}}},
		},
	}
}

func body(t *testing.T, addr string) string {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}

	res, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	b, _ := io.ReadAll(res.Body)

	return string(b)
}

func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	return l.Addr().String()
}

func addr(t *testing.T, m *Manager, name string) string {
	t.Helper()

	a, ok := m.Addr(name)
	if !ok {
		t.Fatalf("entrypoint %s not bound", name)
	}

	return a.String()
}

func TestManagerAddRemoveEntrypoint(t *testing.T) {
	m := newTestManager(t)

	if err := m.Apply(helloServer(map[string]EntryPoint{"a": {Address: "127.0.0.1:0"}}, "one")); err != nil {
		t.Fatal(err)
	}
	addrA := addr(t, m, "a")

	if got := body(t, addrA); got != "one" {
		t.Fatalf("got %q", got)
	}

	// add entrypoint b, a keeps its socket
	if err := m.Apply(helloServer(map[string]EntryPoint{"a": {Address: "127.0.0.1:0"}, "b": {Address: "127.0.0.1:0"}}, "two")); err != nil {
		t.Fatal(err)
	}

	if addr(t, m, "a") != addrA {
		t.Fatal("unchanged entrypoint was rebound")
	}
	if got := body(t, addrA); got != "two" {
		t.Fatalf("a got %q", got)
	}
	if got := body(t, addr(t, m, "b")); got != "two" {
		t.Fatalf("b got %q", got)
	}

	// remove a
	if err := m.Apply(helloServer(map[string]EntryPoint{"b": {Address: "127.0.0.1:0"}}, "three")); err != nil {
		t.Fatal(err)
	}

	if _, ok := m.Addr("a"); ok {
		t.Fatal("entrypoint a still bound")
	}
	if _, err := net.DialTimeout("tcp", addrA, time.Second); err == nil {
		t.Fatal("removed entrypoint still accepts connections")
	}
}

func TestManagerChangeAddress(t *testing.T) {
	m := newTestManager(t)

	first := freeAddr(t)
	if err := m.Apply(helloServer(map[string]EntryPoint{"a": {Address: first}}, "x")); err != nil {
		t.Fatal(err)
	}

	second := freeAddr(t)
	if err := m.Apply(helloServer(map[string]EntryPoint{"a": {Address: second}}, "x")); err != nil {
		t.Fatal(err)
	}

	if got := body(t, second); got != "x" {
		t.Fatalf("got %q", got)
	}
	if _, err := net.DialTimeout("tcp", first, time.Second); err == nil {
		t.Fatal("old address still listening")
	}
}

func TestManagerFailedApplyKeepsRunning(t *testing.T) {
	m := newTestManager(t)

	if err := m.Apply(helloServer(map[string]EntryPoint{"a": {Address: "127.0.0.1:0"}}, "ok")); err != nil {
		t.Fatal(err)
	}
	addrA := addr(t, m, "a")

	// occupied port fails binding of the new entrypoint
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()

	bad := helloServer(map[string]EntryPoint{"a": {Address: "127.0.0.1:0"}, "b": {Address: busy.Addr().String()}}, "bad")
	if err := m.Apply(bad); err == nil {
		t.Fatal("expected bind error")
	}

	// router referencing a missing middleware
	bad = helloServer(map[string]EntryPoint{"a": {Address: "127.0.0.1:0"}}, "bad")
	bad.HTTP.Routers["a"] = shttp.Router{Path: []string{"/*"}, Middlewares: []string{"missing"}}
	if err := m.Apply(bad); err == nil {
		t.Fatal("expected router error")
	}

	if got := body(t, addrA); got != "ok" {
		t.Fatalf("previous config not kept: %q", got)
	}
}

func TestManagerSwitchHTTPToTCP(t *testing.T) {
	m := newTestManager(t)

	upstream := helloServer(map[string]EntryPoint{"up": {Address: "127.0.0.1:0"}, "front": {Address: "127.0.0.1:0"}}, "via")
	if err := m.Apply(upstream); err != nil {
		t.Fatal(err)
	}
	front := addr(t, m, "front")
	up := addr(t, m, "up")

	// front becomes a raw tcp forwarder to up
	next := helloServer(map[string]EntryPoint{"up": {Address: "127.0.0.1:0"}, "front": {Address: "127.0.0.1:0"}}, "via")
	delete(next.HTTP.Routers, "front")
	next.TCP = tcp.TCP{
		Routers:     map[string]tcp.Router{"front": {EntryPoints: []string{"front"}, Middlewares: []string{"fwd"}}},
		Middlewares: map[string]tcp.TCPMiddleware{"fwd": {RedirectMiddleware: &redirect.Redirect{Address: up}}},
	}

	if err := m.Apply(next); err != nil {
		t.Fatal(err)
	}

	if addr(t, m, "front") != front {
		t.Fatal("entrypoint rebound")
	}
	if got := body(t, front); got != "via" {
		t.Fatalf("got %q", got)
	}
}
