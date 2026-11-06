package args

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

func configYAML(addr, message string) string {
	return `server:
  entrypoints:
    web:
      address: "` + addr + `"
  http:
    middlewares:
      hi:
        hello:
          message: "` + message + `"
    routers:
      r:
        path: /*
        middlewares: [hi]
`
}

func waitBody(t *testing.T, addr, want string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		got := body(t, addr)
		if got == want {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("got %q, want %q", got, want)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func TestReloadWatchFile(t *testing.T) {
	prevDrain := registry.DrainTimeout
	registry.DrainTimeout = 0

	prevApp := config.Application
	prevManager := server.GlobalManager

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	t.Cleanup(func() {
		if server.GlobalManager != nil {
			server.GlobalManager.Shutdown()
		}

		cancel()
		wg.Wait()

		config.Application = prevApp
		server.GlobalManager = prevManager
		registry.DrainTimeout = prevDrain
	})

	file := filepath.Join(t.TempDir(), "turna.yaml")
	t.Setenv("CONFIG_FILE", file)

	addr := freeAddr(t)
	if err := os.WriteFile(file, []byte(configYAML(addr, "one")), 0o600); err != nil {
		t.Fatal(err)
	}

	config.Application = config.NewApp()
	if err := loadConfig(ctx, func(func(*pflag.Flag)) {}); err != nil {
		t.Fatal(err)
	}

	if err := config.Application.Server.Run(ctx, wg); err != nil {
		t.Fatal(err)
	}

	off := false
	if err := startReload(ctx, wg, config.Reload{Signal: &off, Watch: true, Debounce: 50 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}

	waitBody(t, addr, "one")

	// editors often replace the file instead of writing it
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(configYAML(addr, "two")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, file); err != nil {
		t.Fatal(err)
	}

	waitBody(t, addr, "two")

	// an invalid configuration keeps the running one
	if err := os.WriteFile(file, []byte("server: [broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)
	waitBody(t, addr, "two")

	if err := os.WriteFile(file, []byte(configYAML(addr, "three")), 0o600); err != nil {
		t.Fatal(err)
	}

	waitBody(t, addr, "three")
}

func TestReloadSignal(t *testing.T) {
	prevDrain := registry.DrainTimeout
	registry.DrainTimeout = 0

	prevApp := config.Application
	prevManager := server.GlobalManager

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	t.Cleanup(func() {
		if server.GlobalManager != nil {
			server.GlobalManager.Shutdown()
		}

		cancel()
		wg.Wait()

		config.Application = prevApp
		server.GlobalManager = prevManager
		registry.DrainTimeout = prevDrain
	})

	file := filepath.Join(t.TempDir(), "turna.yaml")
	t.Setenv("CONFIG_FILE", file)

	addr := freeAddr(t)
	if err := os.WriteFile(file, []byte(configYAML(addr, "one")), 0o600); err != nil {
		t.Fatal(err)
	}

	config.Application = config.NewApp()
	if err := loadConfig(ctx, func(func(*pflag.Flag)) {}); err != nil {
		t.Fatal(err)
	}

	if err := config.Application.Server.Run(ctx, wg); err != nil {
		t.Fatal(err)
	}

	if err := startReload(ctx, wg, config.Reload{}); err != nil {
		t.Fatal(err)
	}

	waitBody(t, addr, "one")

	if err := os.WriteFile(file, []byte(configYAML(addr, "two")), 0o600); err != nil {
		t.Fatal(err)
	}

	// no watch: the file change alone does nothing
	time.Sleep(200 * time.Millisecond)
	waitBody(t, addr, "one")

	p, _ := os.FindProcess(os.Getpid())
	if err := p.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}

	waitBody(t, addr, "two")
}

func TestReloadIntervalAndMiddleware(t *testing.T) {
	prevDrain := registry.DrainTimeout
	registry.DrainTimeout = 0

	prevApp := config.Application
	prevManager := server.GlobalManager

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	t.Cleanup(func() {
		if server.GlobalManager != nil {
			server.GlobalManager.Shutdown()
		}

		cancel()
		wg.Wait()

		config.Application = prevApp
		server.GlobalManager = prevManager
		registry.DrainTimeout = prevDrain
	})

	file := filepath.Join(t.TempDir(), "turna.yaml")
	t.Setenv("CONFIG_FILE", file)

	addr := freeAddr(t)
	write := func(message string) {
		t.Helper()

		cfg := configYAML(addr, message) + `      admin:
        path: /-/reload
        middlewares: [reload]
    # the reload router is matched before the catch-all
`
		cfg = strings.Replace(cfg, "    middlewares:\n", "    middlewares:\n      reload:\n        reload: {}\n", 1)

		if err := os.WriteFile(file, []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("one")

	config.Application = config.NewApp()
	if err := loadConfig(ctx, func(func(*pflag.Flag)) {}); err != nil {
		t.Fatal(err)
	}

	rememberServer(&config.Application.Server)

	if err := config.Application.Server.Run(ctx, wg); err != nil {
		t.Fatal(err)
	}

	off := false
	if err := startReload(ctx, wg, config.Reload{Signal: &off, Interval: 100 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}

	waitBody(t, addr, "one")

	write("two")
	waitBody(t, addr, "two")

	// unchanged configurations are not applied again
	if changed, err := applyConfig(ctx); err != nil || changed {
		t.Fatalf("unchanged reload: changed=%v err=%v", changed, err)
	}

	client := &http.Client{Timeout: 5 * time.Second}

	res, err := client.Get("http://" + addr + "/-/reload")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d", res.StatusCode)
	}

	write("three")

	res, err = client.Post("http://"+addr+"/-/reload", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d", res.StatusCode)
	}

	if got := body(t, addr); got != "three" {
		t.Fatalf("after reload request got %q", got)
	}

	if err := startReload(ctx, wg, config.Reload{Signal: &off, Interval: -time.Second}); err == nil {
		t.Fatal("negative interval accepted")
	}
}
