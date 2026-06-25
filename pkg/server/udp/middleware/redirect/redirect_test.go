package redirect

import (
	"context"
	"net"
	"testing"
	"time"
)

// startEchoUpstream starts a UDP echo server and returns its address.
func startEchoUpstream(t *testing.T) (string, func()) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		buf := make([]byte, 1024)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}

			_, _ = conn.WriteTo(buf[:n], addr)
		}
	}()

	return conn.LocalAddr().String(), func() {
		conn.Close()
		<-done
	}
}

func TestRedirect_Relay(t *testing.T) {
	upstream, stop := startEchoUpstream(t)
	defer stop()

	m := &Redirect{Address: upstream, Timeout: 2 * time.Second}

	handler, err := m.Middleware(context.Background(), "test")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	// client socket to receive the relayed response.
	clientConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen client: %v", err)
	}
	defer clientConn.Close()

	// server socket the handler writes the response back through.
	serverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen server: %v", err)
	}
	defer serverConn.Close()

	if err := handler(serverConn, clientConn.LocalAddr(), []byte("hello")); err != nil {
		t.Fatalf("handler: %v", err)
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))

	buf := make([]byte, 64)
	n, _, err := clientConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read relayed response: %v", err)
	}

	if got := string(buf[:n]); got != "hello" {
		t.Fatalf("relay = %q, want %q", got, "hello")
	}
}
