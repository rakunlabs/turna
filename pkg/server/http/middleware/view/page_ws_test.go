package view

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/accesslog"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/gzip"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/template"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/try"
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

func TestPageWebSocketUpgrade(t *testing.T) {
	testPageWebSocketUpgrade(t, nil)
}

// TestPageWebSocketUpgradeThroughWrappers runs the upgrade through every
// middleware that wraps the ResponseWriter (accesslog, gzip, template, try)
// to make sure they bypass buffering for websocket requests.
func TestPageWebSocketUpgradeThroughWrappers(t *testing.T) {
	accessLogM := &accesslog.AccessLog{
		Path: accesslog.Path{
			Enabled: []accesslog.Check{{URL: "/**"}},
		},
	}
	accessLogMW, err := accessLogM.Middleware()
	if err != nil {
		t.Fatalf("build accesslog middleware: %v", err)
	}

	gzipMW := (&gzip.Gzip{}).Middleware()

	templateM := &template.Template{RawBody: true, Template: `{{ printf "%s" .body_raw }}`}
	templateMW, err := templateM.Middleware()
	if err != nil {
		t.Fatalf("build template middleware: %v", err)
	}

	tryM := &try.Try{Regex: `^/nomatch$`, Replacement: "/", StatusCodes: "404"}
	tryMW, err := tryM.Middleware()
	if err != nil {
		t.Fatalf("build try middleware: %v", err)
	}

	testPageWebSocketUpgrade(t, []func(http.Handler) http.Handler{
		accessLogMW, gzipMW, templateMW, tryMW,
	})
}

func testPageWebSocketUpgrade(t *testing.T, wrappers []func(http.Handler) http.Handler) {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			wsEchoHandler(w, r)

			return
		}

		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head></head><body><a href="/ws">ws</a></body></html>`))
	}))
	defer upstream.Close()

	m := &View{
		PrefixPath: "/view",
		Info: Info{
			Page: []Page{
				{
					Name: "myapp",
					Path: "myapp",
					URL:  upstream.URL,
					Rewrite: &Rewrite{
						Base:     true,
						Absolute: true,
					},
				},
			},
		},
	}

	mw, err := m.Middleware(context.Background(), "test")
	if err != nil {
		t.Fatalf("build middleware: %v", err)
	}

	handler := mw(nil)
	for i := len(wrappers) - 1; i >= 0; i-- {
		handler = wrappers[i](handler)
	}

	proxy := httptest.NewServer(handler)
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

	// Accept-Encoding is sent by browsers on websocket handshakes too, it must
	// not trigger the gzip middleware to wrap the ResponseWriter.
	_, err = fmt.Fprintf(conn,
		"GET /view/page/myapp/ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
			"Accept-Encoding: gzip, deflate, br\r\n"+
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
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 101 Switching Protocols, got %d: %s", resp.StatusCode, string(body))
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
