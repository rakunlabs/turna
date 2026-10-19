package store

import (
	"context"
	"testing"

	"github.com/rakunlabs/ada/utils/sessions"
)

func TestRedisConnConfig(t *testing.T) {
	cfg := Redis{
		Address:  "redis:6379",
		Username: "user",
		Password: "pass",
		Cluster:  true,
		TLS:      TLSConfig{Enabled: true, CAFile: "ca.pem"},
	}.connConfig()

	if len(cfg.Address) != 1 || cfg.Address[0] != "redis:6379" {
		t.Fatalf("address = %q", cfg.Address)
	}
	if cfg.Username != "user" || cfg.Password != "pass" || !cfg.Cluster {
		t.Fatalf("conn config = %+v", cfg)
	}
	if !cfg.TLS.Enabled || cfg.TLS.CAFile != "ca.pem" {
		t.Fatalf("tls = %+v", cfg.TLS)
	}
}

func TestRedisStoreRequiresAddress(t *testing.T) {
	for _, addr := range []string{"", "  "} {
		if _, err := (Redis{Address: addr}).Store(context.Background(), sessions.Options{}); err == nil {
			t.Fatalf("Store(%q) succeeded, want address error", addr)
		}
	}
}
