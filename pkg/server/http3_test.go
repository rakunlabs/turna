package server

import (
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/hello"
)

func tlsHelloServer(addr, message string, h3 bool) *Server {
	ep := EntryPoint{Address: addr}
	if h3 {
		ep.HTTP3 = &HTTP3{Enabled: true}
	}

	return &Server{
		EntryPoints: map[string]EntryPoint{"websecure": ep},
		HTTP: shttp.HTTP{
			Routers: map[string]shttp.Router{
				"r": {Path: []string{"/*"}, Middlewares: []string{"h3-hello"}, TLS: &struct{}{}},
			},
			Middlewares: map[string]shttp.HTTPMiddleware{"h3-hello": {HelloMiddleware: &hello.Hello{Message: message}}},
		},
	}
}

func h3Get(t *testing.T, addr string) (string, error) {
	t.Helper()

	tr := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // test
	defer tr.Close()

	client := &http.Client{Timeout: 3 * time.Second, Transport: tr}

	res, err := client.Get("https://" + addr + "/")
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()

	if res.ProtoMajor != 3 {
		t.Fatalf("proto = %s", res.Proto)
	}

	b, _ := io.ReadAll(res.Body)

	return string(b), nil
}

func TestHTTP3(t *testing.T) {
	m := newTestManager(t)
	addr := freeAddr(t)

	if err := m.Apply(tlsHelloServer(addr, "one", true)); err != nil {
		t.Fatal(err)
	}

	got, err := h3Get(t, addr)
	if err != nil || got != "one" {
		t.Fatalf("http3 = %q, %v", got, err)
	}

	// TCP announces HTTP/3
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test
		DisableKeepAlives: true,
	}}

	res, err := client.Get("https://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	port := addr[strings.LastIndex(addr, ":"):]
	if alt := res.Header.Get("Alt-Svc"); !strings.Contains(alt, `h3="`+port+`"`) {
		t.Fatalf("Alt-Svc = %q", alt)
	}

	// routers and middlewares change without restarting QUIC
	if err := m.Apply(tlsHelloServer(addr, "two", true)); err != nil {
		t.Fatal(err)
	}

	if got, err := h3Get(t, addr); err != nil || got != "two" {
		t.Fatalf("after reload http3 = %q, %v", got, err)
	}

	// disabling http3 closes the UDP socket
	if err := m.Apply(tlsHelloServer(addr, "three", false)); err != nil {
		t.Fatal(err)
	}

	if _, err := h3Get(t, addr); err == nil {
		t.Fatal("http3 still served after disabling")
	}

	if err := m.Apply(&Server{EntryPoints: map[string]EntryPoint{"u": {Address: freeAddr(t), Network: "udp", HTTP3: &HTTP3{Enabled: true}}}}); err == nil {
		t.Fatal("http3 on udp entrypoint accepted")
	}
}

func TestHTTP3RestartOnSameSocket(t *testing.T) {
	m := newTestManager(t)
	addr := freeAddr(t)

	if err := m.Apply(tlsHelloServer(addr, "one", true)); err != nil {
		t.Fatal(err)
	}

	if got, err := h3Get(t, addr); err != nil || got != "one" {
		t.Fatalf("http3 = %q, %v", got, err)
	}

	// the entrypoint moves to a plain router, the TLS server and QUIC stop
	plain := tlsHelloServer(addr, "plain", true)
	r := plain.HTTP.Routers["r"]
	r.TLS = nil
	plain.HTTP.Routers["r"] = r

	if err := m.Apply(plain); err != nil {
		t.Fatal(err)
	}

	// and back to TLS on the same UDP socket
	if err := m.Apply(tlsHelloServer(addr, "two", true)); err != nil {
		t.Fatal(err)
	}

	if got, err := h3Get(t, addr); err != nil || got != "two" {
		t.Fatalf("http3 after restart = %q, %v", got, err)
	}
}

func TestHTTP3AdvertisePortChange(t *testing.T) {
	m := newTestManager(t)
	addr := freeAddr(t)

	s := tlsHelloServer(addr, "one", true)
	if err := m.Apply(s); err != nil {
		t.Fatal(err)
	}

	s = tlsHelloServer(addr, "one", true)
	ep := s.EntryPoints["websecure"]
	ep.HTTP3.AdvertisePort = 443
	s.EntryPoints["websecure"] = ep

	if err := m.Apply(s); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test
		DisableKeepAlives: true,
	}}

	res, err := client.Get("https://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	if alt := res.Header.Get("Alt-Svc"); !strings.Contains(alt, `h3=":443"`) {
		t.Fatalf("Alt-Svc = %q", alt)
	}
}
