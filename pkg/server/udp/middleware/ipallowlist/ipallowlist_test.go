package ipallowlist

import (
	"context"
	"net"
	"testing"
)

func TestIPAllowList(t *testing.T) {
	m := &IPAllowList{SourceRange: []string{"127.0.0.1/32"}}

	handler, err := m.Middleware(context.Background(), "test")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	allowed := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5353}
	if err := handler(nil, allowed, nil); err != nil {
		t.Fatalf("expected 127.0.0.1 allowed, got: %v", err)
	}

	denied := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5353}
	if err := handler(nil, denied, nil); err == nil {
		t.Fatal("expected 10.0.0.1 denied, got nil error")
	}
}
