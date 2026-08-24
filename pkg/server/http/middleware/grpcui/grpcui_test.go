package grpcui

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// blackHoleListener accepts TCP connections and then never speaks, emulating a
// gRPC target that is reachable but never completes a handshake.
func blackHoleListener(t *testing.T) net.Listener {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var (
		mu    sync.Mutex
		conns []net.Conn
	)

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()

	t.Cleanup(func() {
		l.Close()

		mu.Lock()
		defer mu.Unlock()

		for _, c := range conns {
			c.Close()
		}
	})

	return l
}

// unreachableAddr returns an address that nothing is listening on.
func unreachableAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := l.Addr().String()
	l.Close()

	return addr
}

func TestStartFailsFastOnUnreachableTarget(t *testing.T) {
	m := &GrpcUI{Addr: unreachableAddr(t), ConnectTimeout: 5 * time.Second}

	done := make(chan error, 1)

	go func() {
		_, err := m.Start(context.Background())
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error for an unreachable target")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start blocked on an unreachable target")
	}
}

// A request that is still connecting must not outlive its context, otherwise it
// keeps the handler goroutine alive and delays server shutdown.
func TestStartHonoursCanceledContext(t *testing.T) {
	l := blackHoleListener(t)

	m := &GrpcUI{Addr: l.Addr().String(), ConnectTimeout: time.Minute}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	done := make(chan error, 1)

	go func() {
		_, err := m.Start(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error once the context was canceled")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start ignored context cancellation")
	}
}

func TestStartRespectsConnectTimeout(t *testing.T) {
	l := blackHoleListener(t)

	m := &GrpcUI{Addr: l.Addr().String(), ConnectTimeout: 300 * time.Millisecond}

	done := make(chan error, 1)

	go func() {
		_, err := m.Start(context.Background())
		done <- err
	}()

	select {
	case err := <-done:
		// gRPC reports the expired deadline as a status error, so only the
		// presence of an error is asserted here.
		if err == nil {
			t.Fatal("expected an error once ConnectTimeout expired")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start ignored ConnectTimeout")
	}
}

func TestMiddlewareReportsBadGatewayForBrokenTarget(t *testing.T) {
	m := &GrpcUI{
		Addr:           unreachableAddr(t),
		BasePath:       "/view/grpc/broken",
		ConnectTimeout: 5 * time.Second,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/view/grpc/broken/", nil)

	done := make(chan struct{})

	go func() {
		defer close(done)

		m.Middleware()(nil).ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handler blocked on a broken gRPC target")
	}

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected %d, got %d", http.StatusBadGateway, rec.Code)
	}
}
