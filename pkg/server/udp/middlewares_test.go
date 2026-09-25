package udp

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/netlb"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/ipdenylist"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/loadbalancer"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/log"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/mirror"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/ratelimit"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/telemetry"
)

// udpEcho replies with prefix + data and records received payloads.
func udpEcho(t *testing.T, prefix string) (string, func() []string) {
	t.Helper()

	conn := newServerConn(t)
	t.Cleanup(func() { conn.Close() })

	var (
		mu  sync.Mutex
		got []string
	)

	go func() {
		buf := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}

			mu.Lock()
			got = append(got, string(buf[:n]))
			mu.Unlock()

			_, _ = conn.WriteTo(append([]byte(prefix), buf[:n]...), addr)
		}
	}()

	return conn.LocalAddr().String(), func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), got...)
	}
}

type udpFactory interface {
	Middleware(ctx context.Context, name string) (Handler, error)
}

func startUDP(t *testing.T, factories ...udpFactory) string {
	t.Helper()

	conn := newServerConn(t)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	middlewares := make([]Middleware, 0, len(factories))

	for _, f := range factories {
		h, err := f.Middleware(ctx, "test")
		if err != nil {
			t.Fatal(err)
		}

		middlewares = append(middlewares, Middleware{Name: "test", Conn: h})
	}

	serve(ctx, wg, "test", conn, middlewares)

	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})

	return conn.LocalAddr().String()
}

func exchange(t *testing.T, client net.Conn, msg string, timeout time.Duration) (string, error) {
	t.Helper()

	if _, err := client.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}

	_ = client.SetReadDeadline(time.Now().Add(timeout))

	buf := make([]byte, 1024)

	n, err := client.Read(buf)

	return string(buf[:n]), err
}

func dialUDP(t *testing.T, addr string) net.Conn {
	t.Helper()

	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { c.Close() })

	return c
}

func TestUDPLoadBalancerSessions(t *testing.T) {
	upA, _ := udpEcho(t, "a:")
	upB, _ := udpEcho(t, "b:")

	addr := startUDP(t, &loadbalancer.LoadBalancer{Servers: []netlb.Server{{Address: upA}, {Address: upB}}})

	seen := map[string]int{}

	for range 4 {
		client := dialUDP(t, addr)

		first, err := exchange(t, client, "x", 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}

		// the same client stays on the same upstream
		for range 3 {
			got, err := exchange(t, client, "x", 2*time.Second)
			if err != nil || got != first {
				t.Fatalf("session moved: %q -> %q (%v)", first, got, err)
			}
		}

		seen[first]++
	}

	if seen["a:x"] != 2 || seen["b:x"] != 2 {
		t.Fatalf("distribution = %v", seen)
	}
}

func TestUDPLoadBalancerHealthCheck(t *testing.T) {
	up, _ := udpEcho(t, "up:")

	dead := newServerConn(t)
	deadAddr := dead.LocalAddr().String()
	dead.Close()

	addr := startUDP(t, &loadbalancer.LoadBalancer{
		Servers: []netlb.Server{{Address: deadAddr}, {Address: up}},
		HealthCheck: &loadbalancer.HealthCheck{
			Interval: 50 * time.Millisecond,
			Timeout:  100 * time.Millisecond,
			Payload:  "ping",
		},
	})

	// wait for the dead upstream to be marked unhealthy
	time.Sleep(300 * time.Millisecond)

	for range 4 {
		got, err := exchange(t, dialUDP(t, addr), "x", 2*time.Second)
		if err != nil || got != "up:x" {
			t.Fatalf("got %q %v", got, err)
		}
	}
}

func TestUDPMirror(t *testing.T) {
	up, _ := udpEcho(t, "main:")
	shadow, shadowGot := udpEcho(t, "shadow:")

	addr := startUDP(t,
		&mirror.Mirror{Servers: []mirror.Server{{Address: shadow}}},
		&loadbalancer.LoadBalancer{Servers: []netlb.Server{{Address: up}}},
	)

	client := dialUDP(t, addr)

	got, err := exchange(t, client, "hello", 2*time.Second)
	if err != nil || got != "main:hello" {
		t.Fatalf("got %q %v", got, err)
	}

	// no shadow reply reaches the client
	if extra, err := exchange(t, client, "again", 300*time.Millisecond); err != nil || extra != "main:again" {
		t.Fatalf("second reply %q %v", extra, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for strings.Join(shadowGot(), ",") != "hello,again" {
		if time.Now().After(deadline) {
			t.Fatalf("shadow got %v", shadowGot())
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestUDPRateLimitAndDenyList(t *testing.T) {
	addr := startUDP(t, &ratelimit.RateLimit{Rate: 0.001, Burst: 2}, echoFactory{})
	client := dialUDP(t, addr)

	for i := range 2 {
		if _, err := exchange(t, client, "x", 2*time.Second); err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
	}

	if _, err := exchange(t, client, "x", 300*time.Millisecond); err == nil {
		t.Fatal("third packet should be dropped")
	}

	denied := startUDP(t, &ipdenylist.IPDenyList{SourceRange: []string{"127.0.0.1"}}, echoFactory{})
	if _, err := exchange(t, dialUDP(t, denied), "x", 300*time.Millisecond); err == nil {
		t.Fatal("denied packet answered")
	}
}

func TestUDPLogAndTelemetry(t *testing.T) {
	addr := startUDP(t, &log.Log{}, &telemetry.Telemetry{}, echoFactory{})

	if got, err := exchange(t, dialUDP(t, addr), "x", 2*time.Second); err != nil || got != "x" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestUDPConfigErrors(t *testing.T) {
	for name, f := range map[string]udpFactory{
		"rate_limit":    &ratelimit.RateLimit{},
		"mirror":        &mirror.Mirror{},
		"load_balancer": &loadbalancer.LoadBalancer{},
		"health_check":  &loadbalancer.LoadBalancer{Servers: []netlb.Server{{Address: "127.0.0.1:1"}}, HealthCheck: &loadbalancer.HealthCheck{}},
		"log":           &log.Log{Level: "loud"},
	} {
		if _, err := f.Middleware(t.Context(), "test"); err == nil {
			t.Errorf("%s: expected config error", name)
		}
	}
}

type echoFactory struct{}

func (echoFactory) Middleware(context.Context, string) (Handler, error) { return echoHandler, nil }
