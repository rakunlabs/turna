package service

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// wsEchoHandler performs a minimal WebSocket-style upgrade handshake (without
// frame parsing) and echoes raw bytes back to the caller.
func wsEchoHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "expected websocket upgrade", http.StatusBadRequest)

		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)

		return
	}

	conn, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
	_ = brw.Flush()

	buf := make([]byte, 512)
	for {
		n, err := brw.Read(buf)
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func newProxyServer(t *testing.T, upstreamURL string) *httptest.Server {
	return newProxyServerWithConfig(t, &Service{
		LoadBalancer: LoadBalancer{
			Servers: []Server{{URL: upstreamURL}},
		},
	})
}

func newProxyServerWithConfig(t *testing.T, m *Service) *httptest.Server {
	t.Helper()

	mws, err := m.Middleware()
	if err != nil {
		t.Fatalf("build middleware: %v", err)
	}

	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	for i := len(mws) - 1; i >= 0; i-- {
		handler = mws[i](handler)
	}

	return httptest.NewServer(handler)
}

func TestServiceUsesForwardProxy(t *testing.T) {
	var gotURL string
	passHostHeader := false
	forwardProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = w.Write([]byte("from forward proxy"))
	}))
	defer forwardProxy.Close()

	proxy := newProxyServerWithConfig(t, &Service{
		Proxy:          forwardProxy.URL,
		PassHostHeader: &passHostHeader,
		LoadBalancer: LoadBalancer{
			Servers: []Server{{URL: "http://portal.ai.pubc.worldline-solutions.com"}},
		},
	})
	defer proxy.Close()

	response, err := http.Get(proxy.URL + "/dashboard?team=turna")
	if err != nil {
		t.Fatalf("request service: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(body) != "from forward proxy" {
		t.Fatalf("expected forward proxy response, got %q", body)
	}
	if want := "http://portal.ai.pubc.worldline-solutions.com/dashboard?team=turna"; gotURL != want {
		t.Fatalf("expected proxy request URL %q, got %q", want, gotURL)
	}
}

func TestServiceRejectsInvalidForwardProxyScheme(t *testing.T) {
	m := &Service{
		Proxy: "socks5://proxy.internal:1080",
		LoadBalancer: LoadBalancer{
			Servers: []Server{{URL: "https://portal.ai.pubc.worldline-solutions.com"}},
		},
	}

	if _, err := m.Middleware(); err == nil {
		t.Fatal("expected invalid proxy scheme error")
	}
}

func TestProxyWebSocketUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(wsEchoHandler))
	defer upstream.Close()

	proxy := newProxyServer(t, upstream.URL)
	defer proxy.Close()

	u, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = fmt.Fprintf(conn,
		"GET / HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
			"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n",
		u.Host)
	if err != nil {
		t.Fatalf("write upgrade request: %v", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read upgrade response: %v", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}

	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	got := make([]byte, 4)
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}

	if string(got) != "ping" {
		t.Fatalf("expected echo %q, got %q", "ping", string(got))
	}
}
