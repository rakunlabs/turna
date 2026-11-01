package loadbalancer

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/cert"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
	"github.com/rakunlabs/turna/pkg/server/netlb"
)

// serve accepts connections on l and answers one line with prefix.
func serve(t *testing.T, l net.Listener, prefix string) {
	t.Helper()
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go func() {
				defer func() { _ = c.Close() }()

				line, _ := bufio.NewReader(c).ReadString('\n')
				_, _ = io.WriteString(c, prefix+line)
			}()
		}
	}()
}

func tlsUpstream(t *testing.T) (string, string) {
	t.Helper()

	generated, err := cert.GenerateCertificate(cert.WithDNSNames("backend.example.com"))
	if err != nil {
		t.Fatal(err)
	}

	pair, err := tls.X509KeyPair(generated.Certificate, generated.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}

	serve(t, l, "tls:")

	return l.Addr().String(), string(generated.Certificate)
}

// exchange runs the middleware for one client connection and returns the
// answer and the middleware error.
func exchange(t *testing.T, lb *LoadBalancer) (string, error) {
	t.Helper()

	mw, err := lb.Middleware(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}

	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	done := make(chan error, 1)
	go func() {
		done <- mw(nil)(server)
		_ = server.Close()
	}()

	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(client, "hi\n")

	line, _ := bufio.NewReader(client).ReadString('\n')

	return strings.TrimSpace(line), <-done
}

func TestLoadBalancerTLS(t *testing.T) {
	addr, ca := tlsUpstream(t)

	got, err := exchange(t, &LoadBalancer{
		Servers: []netlb.Server{{Address: addr}},
		TLS:     []UpstreamTLS{{Addresses: []string{addr}, TLS: &service.UpstreamTLS{CA: ca, ServerName: "backend.example.com"}}},
	})
	if err != nil || got != "tls:hi" {
		t.Fatalf("answer = %q, err = %v", got, err)
	}
}

func TestLoadBalancerTLSUntrusted(t *testing.T) {
	addr, _ := tlsUpstream(t)
	_, other := tlsUpstream(t)

	got, err := exchange(t, &LoadBalancer{
		Servers: []netlb.Server{{Address: addr}},
		TLS:     []UpstreamTLS{{Addresses: []string{addr}, TLS: &service.UpstreamTLS{CA: other, ServerName: "backend.example.com"}}},
	})
	if err == nil || got != "" {
		t.Fatalf("untrusted upstream: answer = %q, err = %v", got, err)
	}
}

func TestLoadBalancerTLSOtherUpstreamsUnchanged(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	serve(t, l, "plain:")

	got, err := exchange(t, &LoadBalancer{
		Servers: []netlb.Server{{Address: l.Addr().String()}},
		TLS:     []UpstreamTLS{{Addresses: []string{"127.0.0.1:1"}, TLS: &service.UpstreamTLS{ServerName: "backend.example.com"}}},
	})
	if err != nil || got != "plain:hi" {
		t.Fatalf("answer = %q, err = %v", got, err)
	}
}

func TestLoadBalancerTLSHandshakeTimeout(t *testing.T) {
	// accepts connections but never answers the handshake
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			t.Cleanup(func() { _ = c.Close() })
		}
	}()

	start := time.Now()

	_, err = exchange(t, &LoadBalancer{
		Servers:     []netlb.Server{{Address: l.Addr().String()}},
		DialTimeout: 200 * time.Millisecond,
		TLS:         []UpstreamTLS{{Addresses: []string{l.Addr().String()}, TLS: &service.UpstreamTLS{ServerName: "backend.example.com"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "tls handshake") {
		t.Fatalf("err = %v, want a tls handshake error", err)
	}

	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("handshake took %s, want about the dial timeout", d)
	}
}

func TestLoadBalancerTLSInvalidCA(t *testing.T) {
	_, err := (&LoadBalancer{
		Servers: []netlb.Server{{Address: "127.0.0.1:1"}},
		TLS:     []UpstreamTLS{{Addresses: []string{"127.0.0.1:1"}, TLS: &service.UpstreamTLS{CA: "not a certificate"}}},
	}).Middleware(context.Background(), "test")
	if err == nil {
		t.Fatal("invalid ca accepted")
	}
}
