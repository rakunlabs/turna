package mirror

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// listen accepts one connection and returns everything it receives.
func listen(t *testing.T) (string, <-chan []byte) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	got := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()

		_, _ = c.Write([]byte("ignored reply"))
		b, _ := io.ReadAll(c)
		got <- b
	}()

	return ln.Addr().String(), got
}

func TestMirror(t *testing.T) {
	addr, got := listen(t)

	mw, err := (&Mirror{Servers: []Server{{Address: addr}}}).Middleware(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	client, server := net.Pipe()

	var main []byte
	done := make(chan error, 1)
	go func() {
		done <- mw(func(conn net.Conn) error {
			b, err := io.ReadAll(conn)
			main = b

			return err
		})(server)
	}()

	_, _ = client.Write([]byte("hello "))
	_, _ = client.Write([]byte("world"))
	client.Close()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if string(main) != "hello world" {
		t.Fatalf("main got %q", main)
	}

	select {
	case b := <-got:
		if string(b) != "hello world" {
			t.Fatalf("mirror got %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mirror got nothing")
	}
}

func TestUnreachableShadow(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	mw, err := (&Mirror{Servers: []Server{{Address: addr}}, DialTimeout: 100 * time.Millisecond}).Middleware(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	client, server := net.Pipe()
	go func() {
		_, _ = client.Write([]byte("data"))
		client.Close()
	}()

	var main []byte
	_ = mw(func(conn net.Conn) error {
		main, _ = io.ReadAll(conn)

		return nil
	})(server)

	if string(main) != "data" {
		t.Fatalf("main got %q", main)
	}
}

func TestShadowOverflow(t *testing.T) {
	s := &shadow{limit: 4, wake: make(chan struct{}, 1)}

	s.send([]byte("abc"))
	s.send([]byte("de"))

	if _, stop, _ := s.take(); !stop {
		t.Fatal("overflow did not stop the shadow")
	}

	s.send([]byte("x"))
	if data, _, _ := s.take(); len(data) != 0 {
		t.Fatal("dropped shadow queued data")
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*Mirror{
		{},
		{Servers: []Server{{Address: "127.0.0.1:1", Percent: 101}}},
		{Servers: []Server{{Address: "127.0.0.1:1"}}, Buffer: -1},
	} {
		if _, err := m.Middleware(context.Background(), ""); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}

var _ tcpmw.Unwrapper = (*mirrorConn)(nil)
