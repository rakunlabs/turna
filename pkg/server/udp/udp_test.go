package udp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func echoHandler(conn net.PacketConn, addr net.Addr, data []byte) error {
	_, err := conn.WriteTo(data, addr)

	return err
}

func newServerConn(t *testing.T) net.PacketConn {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen packet: %v", err)
	}

	return conn
}

func TestServe_Echo(t *testing.T) {
	conn := newServerConn(t)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	serve(ctx, wg, "test", conn, []Middleware{{Name: "echo", Conn: echoHandler}})

	client, err := net.Dial("udp", conn.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))

	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if got := string(buf[:n]); got != "ping" {
		t.Fatalf("echo = %q, want %q", got, "ping")
	}

	cancel()
	wg.Wait()
}

func TestServe_ChainStops(t *testing.T) {
	conn := newServerConn(t)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	// A filter that drops everything before the echo handler runs.
	deny := func(_ net.PacketConn, _ net.Addr, _ []byte) error {
		return errors.New("denied")
	}

	serve(ctx, wg, "test", conn, []Middleware{
		{Name: "deny", Conn: deny},
		{Name: "echo", Conn: echoHandler},
	})

	client, err := net.Dial("udp", conn.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = client.SetReadDeadline(time.Now().Add(300 * time.Millisecond))

	buf := make([]byte, 64)
	if _, err := client.Read(buf); err == nil {
		t.Fatal("expected no response when chain stops, got one")
	}

	cancel()
	wg.Wait()
}
