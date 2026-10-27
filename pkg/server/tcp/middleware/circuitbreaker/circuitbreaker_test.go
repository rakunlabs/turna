package circuitbreaker

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/breaker"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

func TestCircuitBreaker(t *testing.T) {
	mw, err := (&CircuitBreaker{Config: breaker.Config{MinRequests: 2, OpenDuration: time.Hour}}).Middleware(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	var fail error
	calls := 0
	h := mw(func(net.Conn) error {
		calls++

		return fail
	})

	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	if err := h(server); err != nil {
		t.Fatal(err)
	}

	// rejections of later filters are not failures
	fail = tcpmw.ErrReject
	for range 3 {
		_ = h(server)
	}

	fail = errors.New("dial failed")
	_ = h(server)
	_ = h(server)

	before := calls
	err = h(server)
	if !errors.Is(err, tcpmw.ErrReject) || calls != before {
		t.Fatalf("open circuit: err %v, calls %d -> %d", err, before, calls)
	}
}

func TestInvalidConfig(t *testing.T) {
	if _, err := (&CircuitBreaker{Config: breaker.Config{FailureRatio: 2}}).Middleware(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	}
}
