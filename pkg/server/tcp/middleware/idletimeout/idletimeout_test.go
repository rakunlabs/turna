package idletimeout

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// Clearing the read deadline after a header peek keeps the idle timeout.
func TestClearedDeadlineKeepsIdleTimeout(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	c := &Conn{Conn: server, timeout: 50 * time.Millisecond}
	c.extend()

	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	_ = c.SetReadDeadline(time.Time{})

	done := make(chan error, 1)
	go func() {
		_, err := c.Read(make([]byte, 1))
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("read error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle timeout was cleared")
	}
}
