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
