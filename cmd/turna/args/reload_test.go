package args

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	return l.Addr().String()
}

func serverData(addr, message string) map[string]any {
	return map[string]any{
		"entrypoints": map[string]any{"web": map[string]any{"address": addr}},
		"http": map[string]any{
			"middlewares": map[string]any{"hi": map[string]any{"hello": map[string]any{"message": message}}},
			"routers":     map[string]any{"r": map[string]any{"path": []any{"/*"}, "middlewares": []any{"hi"}}},
		},
	}
}

func body(t *testing.T, addr string) string {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}

	res, err := client.Get("http://" + addr + "/")
	if err != nil {
		return "error: " + err.Error()
	}
	defer func() { _ = res.Body.Close() }()

	b, _ := io.ReadAll(res.Body)

	return string(b)
}

func TestReloadServerFromLoadValue(t *testing.T) {
	prevDrain := registry.DrainTimeout
	registry.DrainTimeout = 0

	prevServer := config.Application.Server
	prevManager := server.GlobalManager

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	t.Cleanup(func() {
		if server.GlobalManager != nil {
			server.GlobalManager.Shutdown()
		}

		cancel()
		wg.Wait()

		config.Application.Server = prevServer
		server.GlobalManager = prevManager
		registry.DrainTimeout = prevDrain
	})

	first := freeAddr(t)
	data := map[string]any{"srv": serverData(first, "one")}

	config.Application.Server = server.Server{LoadValue: "srv"}
	if err := config.Decode(data["srv"], &config.Application.Server); err != nil {
		t.Fatal(err)
	}

	rememberLoadValue(data)

	if err := config.Application.Server.Run(ctx, wg); err != nil {
		t.Fatal(err)
	}

	if got := body(t, first); got != "one" {
		t.Fatalf("got %q", got)
	}

	// new address and message
	second := freeAddr(t)
	reloadServer(map[string]any{"srv": serverData(second, "two")})

	if got := body(t, second); got != "two" {
		t.Fatalf("after reload got %q", got)
	}

	if _, err := net.DialTimeout("tcp", first, 200*time.Millisecond); err == nil {
		t.Fatal("old entrypoint still open")
	}

	// invalid config keeps the running one
	bad := serverData(second, "three")
	bad["http"].(map[string]any)["routers"] = map[string]any{"r": map[string]any{"path": []any{"/*"}, "middlewares": []any{"missing"}}}
	reloadServer(map[string]any{"srv": bad})

	if got := body(t, second); got != "two" {
		t.Fatalf("invalid reload changed server: %q", got)
	}
}
