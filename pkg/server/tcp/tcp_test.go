package tcp

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/netlb"
	"github.com/rakunlabs/turna/pkg/server/proxyproto"
	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/bandwidth"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/connlimit"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/httpconnect"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/idletimeout"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/ipdenylist"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/loadbalancer"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/proxyprotocol"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/ratelimit"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/redirect"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/snirouter"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/telemetry"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/tlsterminate"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// echoServer replies with prefix + each line it receives.
func echoServer(t *testing.T, prefix string) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go func() {
				defer c.Close()

				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}

					fmt.Fprint(c, prefix+line)
				}
			}()
		}
	}()

	return l.Addr().String()
}

// startChain serves the chain on a new listener and returns its address.
func startChain(t *testing.T, middlewares ...tcpmw.Middleware) string {
	t.Helper()

	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	t.Cleanup(func() {
		cancel()
		l.Close()
		wg.Wait()
	})

	serve(ctx, wg, "test", l, tcpmw.Chain(func(net.Conn) error { return nil }, middlewares...))

	return l.Addr().String()
}

type factory interface {
	Middleware(ctx context.Context, name string) (tcpmw.Middleware, error)
}

func mw(t *testing.T, f factory) tcpmw.Middleware {
	t.Helper()

	m, err := f.Middleware(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func roundTrip(t *testing.T, conn net.Conn, msg string) (string, error) {
	t.Helper()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := fmt.Fprint(conn, msg+"\n"); err != nil {
		return "", err
	}

	line, err := bufio.NewReader(conn).ReadString('\n')

	return strings.TrimSuffix(line, "\n"), err
}

func dialRoundTrip(t *testing.T, addr, msg string) (string, error) {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	return roundTrip(t, conn, msg)
}

func TestChainOrderAndRedirect(t *testing.T) {
	up := echoServer(t, "up:")

	var order []string
	var mu sync.Mutex

	record := func(name string) tcpmw.Middleware {
		return func(next tcpmw.Handler) tcpmw.Handler {
			return func(conn net.Conn) error {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()

				return next(conn)
			}
		}
	}

	addr := startChain(t, record("a"), record("b"), mw(t, &redirect.Redirect{Address: up}))

	got, err := dialRoundTrip(t, addr, "hi")
	if err != nil || got != "up:hi" {
		t.Fatalf("got %q %v", got, err)
	}

	mu.Lock()
	defer mu.Unlock()

	if strings.Join(order, ",") != "a,b" {
		t.Fatalf("order = %v", order)
	}
}

func TestIPDenyList(t *testing.T) {
	up := echoServer(t, "")
	addr := startChain(t, mw(t, &ipdenylist.IPDenyList{SourceRange: []string{"127.0.0.0/8"}}), mw(t, &redirect.Redirect{Address: up}))

	if _, err := dialRoundTrip(t, addr, "hi"); err == nil {
		t.Fatal("expected connection to be closed")
	}
}

func TestRateLimit(t *testing.T) {
	up := echoServer(t, "")
	addr := startChain(t, mw(t, &ratelimit.RateLimit{Rate: 0.001, Burst: 2}), mw(t, &redirect.Redirect{Address: up}))

	for i := range 2 {
		if _, err := dialRoundTrip(t, addr, "hi"); err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
	}

	if _, err := dialRoundTrip(t, addr, "hi"); err == nil {
		t.Fatal("third connection should be rejected")
	}
}

func TestConnLimit(t *testing.T) {
	up := echoServer(t, "")
	addr := startChain(t, mw(t, &connlimit.ConnLimit{MaxPerIP: 1}), mw(t, &redirect.Redirect{Address: up}))

	first, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	if _, err := roundTrip(t, first, "hi"); err != nil {
		t.Fatal(err)
	}

	if _, err := dialRoundTrip(t, addr, "hi"); err == nil {
		t.Fatal("second concurrent connection should be rejected")
	}

	first.Close()

	// released after the first connection finishes
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := dialRoundTrip(t, addr, "hi"); err == nil {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("limit not released")
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func TestIdleTimeout(t *testing.T) {
	up := echoServer(t, "")
	addr := startChain(t, mw(t, &idletimeout.IdleTimeout{Timeout: 200 * time.Millisecond}), mw(t, &redirect.Redirect{Address: up}))

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := roundTrip(t, conn, "hi"); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	start := time.Now()
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}

	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("closed after %s", d)
	}
}

func TestBandwidthLimit(t *testing.T) {
	up := echoServer(t, "")
	addr := startChain(t, mw(t, &bandwidth.BandwidthLimit{Download: 1000, Burst: 100}), mw(t, &redirect.Redirect{Address: up}))

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	start := time.Now()

	// 400 bytes at 1000 B/s with a 100 byte burst takes about 300ms
	if _, err := roundTrip(t, conn, strings.Repeat("x", 399)); err != nil {
		t.Fatal(err)
	}

	if d := time.Since(start); d < 200*time.Millisecond {
		t.Fatalf("not limited, took %s", d)
	}
}

func TestProxyProtocolIncomingAndOutgoing(t *testing.T) {
	// upstream reads the PROXY header sent by redirect and echoes the source
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go func() {
				defer c.Close()

				r := bufio.NewReader(c)

				h, err := proxyproto.Read(r)
				if err != nil {
					fmt.Fprintf(c, "err %v\n", err)

					return
				}

				fmt.Fprintf(c, "%s\n", h.Source)
			}()
		}
	}()

	addr := startChain(t,
		mw(t, &proxyprotocol.ProxyProtocol{TrustedIPs: []string{"127.0.0.1"}}),
		mw(t, &redirect.Redirect{Address: l.Addr().String(), ProxyProtocol: true, ProxyProtocolVersion: 2}),
	)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	src := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 5555}
	if err := proxyproto.Write(conn, 1, src, conn.RemoteAddr()); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(line) != src.String() {
		t.Fatalf("upstream saw %q, want %s", line, src)
	}
}

func TestProxyProtocolRequired(t *testing.T) {
	up := echoServer(t, "")
	addr := startChain(t,
		mw(t, &proxyprotocol.ProxyProtocol{TrustedIPs: []string{"127.0.0.1"}}),
		mw(t, &redirect.Redirect{Address: up}),
	)

	if _, err := dialRoundTrip(t, addr, "hi"); err == nil {
		t.Fatal("connection without header should be rejected")
	}

	optional := startChain(t,
		mw(t, &proxyprotocol.ProxyProtocol{TrustedIPs: []string{"127.0.0.1"}, Optional: true}),
		mw(t, &redirect.Redirect{Address: up}),
	)

	if got, err := dialRoundTrip(t, optional, "hi"); err != nil || got != "hi" {
		t.Fatalf("optional: %q %v", got, err)
	}
}

func TestTLSTerminate(t *testing.T) {
	up := echoServer(t, "plain:")
	addr := startChain(t, mw(t, &tlsterminate.TLSTerminate{}), mw(t, &redirect.Redirect{Address: up}))

	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // test
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	got, err := roundTrip(t, conn, "hi")
	if err != nil || got != "plain:hi" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestSNIRouter(t *testing.T) {

	upA := echoServer(t, "a:")
	upB := echoServer(t, "b:")

	registry.GlobalReg.AddTcpMiddleware("term-a", []tcpmw.Middleware{mw(t, &tlsterminate.TLSTerminate{}), mw(t, &redirect.Redirect{Address: upA})})
	registry.GlobalReg.AddTcpMiddleware("term-b", []tcpmw.Middleware{mw(t, &tlsterminate.TLSTerminate{}), mw(t, &redirect.Redirect{Address: upB})})

	addr := startChain(t, mw(t, &snirouter.SNIRouter{
		Routes: []snirouter.Route{
			{SNI: []string{"a.example.com"}, Middlewares: []string{"term-a"}},
			{SNI: []string{"*.b.example.com"}, Middlewares: []string{"term-b"}},
		},
	}))

	for sni, want := range map[string]string{"a.example.com": "a:hi", "x.b.example.com": "b:hi"} {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: sni, InsecureSkipVerify: true}) //nolint:gosec // test
		if err != nil {
			t.Fatalf("%s: %v", sni, err)
		}

		got, err := roundTrip(t, conn, "hi")
		conn.Close()

		if err != nil || got != want {
			t.Fatalf("%s: got %q %v", sni, got, err)
		}
	}

	if _, err := tls.Dial("tcp", addr, &tls.Config{ServerName: "other.example.com", InsecureSkipVerify: true}); err == nil { //nolint:gosec // test
		t.Fatal("unknown sni should be rejected")
	}
}

func TestLoadBalancer(t *testing.T) {
	upA := echoServer(t, "a:")
	upB := echoServer(t, "b:")

	addr := startChain(t, mw(t, &loadbalancer.LoadBalancer{
		Servers: []netlb.Server{{Address: upA, Weight: 2}, {Address: upB}},
	}))

	counts := map[string]int{}

	for range 6 {
		got, err := dialRoundTrip(t, addr, "hi")
		if err != nil {
			t.Fatal(err)
		}

		counts[got]++
	}

	if counts["a:hi"] != 4 || counts["b:hi"] != 2 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestLoadBalancerRetryAndHealthCheck(t *testing.T) {
	up := echoServer(t, "up:")

	// reserve an address with nothing listening
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	deadAddr := dead.Addr().String()
	dead.Close()

	addr := startChain(t, mw(t, &loadbalancer.LoadBalancer{
		Servers:     []netlb.Server{{Address: deadAddr}, {Address: up}},
		Retry:       1,
		HealthCheck: &netlb.HealthCheck{Interval: 50 * time.Millisecond, Timeout: 100 * time.Millisecond},
	}))

	for range 4 {
		got, err := dialRoundTrip(t, addr, "hi")
		if err != nil || got != "up:hi" {
			t.Fatalf("got %q %v", got, err)
		}
	}
}

func TestHTTPConnect(t *testing.T) {
	up := echoServer(t, "tunnel:")
	_, port, _ := net.SplitHostPort(up)

	addr := startChain(t, mw(t, &httpconnect.HTTPConnect{
		Users:        map[string]string{"user": "pass"},
		AllowedHosts: []string{"127.0.0.1"},
	}))

	connect := func(target, auth string) (net.Conn, *bufio.Reader, int) {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}

		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
		if auth != "" {
			fmt.Fprintf(conn, "Proxy-Authorization: Basic %s\r\n", base64.StdEncoding.EncodeToString([]byte(auth)))
		}
		fmt.Fprint(conn, "\r\n")

		br := bufio.NewReader(conn)

		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			t.Fatal(err)
		}

		return conn, br, resp.StatusCode
	}

	conn, _, code := connect(up, "")
	conn.Close()

	if code != http.StatusProxyAuthRequired {
		t.Fatalf("no auth: %d", code)
	}

	conn, _, code = connect("localhost:"+port, "user:pass")
	conn.Close()

	if code != http.StatusForbidden {
		t.Fatalf("not allowed host: %d", code)
	}

	conn, br, code := connect(up, "user:pass")
	defer conn.Close()

	if code != http.StatusOK {
		t.Fatalf("connect: %d", code)
	}

	fmt.Fprint(conn, "hi\n")

	line, err := br.ReadString('\n')
	if err != nil || line != "tunnel:hi\n" {
		t.Fatalf("tunnel: %q %v", line, err)
	}
}

func TestTelemetry(t *testing.T) {
	up := echoServer(t, "")
	addr := startChain(t, mw(t, &telemetry.Telemetry{}), mw(t, &redirect.Redirect{Address: up}))

	if got, err := dialRoundTrip(t, addr, "hi"); err != nil || got != "hi" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestMiddlewareConfigErrors(t *testing.T) {
	for name, f := range map[string]factory{
		"conn_limit":      &connlimit.ConnLimit{},
		"rate_limit":      &ratelimit.RateLimit{},
		"idle_timeout":    &idletimeout.IdleTimeout{},
		"bandwidth_limit": &bandwidth.BandwidthLimit{},
		"proxy_protocol":  &proxyprotocol.ProxyProtocol{},
		"sni_router":      &snirouter.SNIRouter{},
		"load_balancer":   &loadbalancer.LoadBalancer{},
		"tls_terminate":   &tlsterminate.TLSTerminate{MinVersion: "0.9"},
		"redirect":        &redirect.Redirect{Address: "127.0.0.1:1", ProxyProtocol: true, ProxyProtocolVersion: 3},
	} {
		if _, err := f.Middleware(t.Context(), "test"); err == nil {
			t.Errorf("%s: expected config error", name)
		}
	}
}
