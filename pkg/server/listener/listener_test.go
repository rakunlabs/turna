package listener

import (
	"net"
	"testing"
	"time"
)

// A connection waiting for a replaced listener goes to the new listener.
func TestDeliverAfterListenerSwap(t *testing.T) {
	e := &Entrypoint{Name: "test"}

	old := &Virtual{conns: make(chan net.Conn), done: make(chan struct{})}
	next := &Virtual{conns: make(chan net.Conn), done: make(chan struct{})}
	e.current = old

	server, client := net.Pipe()
	defer client.Close()

	go func() {
		time.Sleep(20 * time.Millisecond)

		e.mu.Lock()
		e.current = next
		e.mu.Unlock()

		old.closeOnce()
	}()

	go e.deliver(server)

	select {
	case got := <-next.conns:
		if got != server {
			t.Fatal("other connection delivered")
		}

		_ = got.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("connection not delivered to the new listener")
	}
}

func TestSameAddress(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{":80", "0.0.0.0:80", true},
		{":80", "[::]:80", true},
		{"127.0.0.1:80", "127.0.0.1:80", true},
		{"[::1]:80", "[0:0:0:0:0:0:0:1]:80", true},
		{":80", ":81", false},
		{"127.0.0.1:80", ":80", false},
		{"localhost:80", "127.0.0.1:80", false},
	} {
		if got := sameAddress(tt.a, tt.b); got != tt.want {
			t.Errorf("sameAddress(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
}
