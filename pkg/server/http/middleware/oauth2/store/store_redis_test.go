package store

import (
	"os"
	"testing"

	"github.com/worldline-go/conn/connredis"
)

// Set STORE_TEST_REDIS=host:port to run against a real Redis.
func redisStore(t *testing.T, prefix string) *StoreCache {
	t.Helper()

	addr := os.Getenv("STORE_TEST_REDIS")
	if addr == "" {
		t.Skip("STORE_TEST_REDIS is not set")
	}

	s, err := (&Store{Active: "redis", Redis: connredis.Config{Address: []string{addr}}, KeyPrefix: prefix}).Init(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	return s
}

// Without key_prefix the Redis keys stay exactly "code_<id>" so existing
// deployments sharing Redis keep interoperating.
func TestRedisDefaultKeysUnprefixed(t *testing.T) {
	s := redisStore(t, "")

	id, err := s.CodeGen(t.Context(), Code{Alias: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.redisClient.Exists(t.Context(), "code_"+id).Result(); err != nil || n != 1 {
		t.Fatalf("raw key code_%s exists = %d, %v", id, n, err)
	}
	if _, ok, err := s.TakeCode(t.Context(), "code_"+id); err != nil || !ok {
		t.Fatalf("take = %v, %v", ok, err)
	}
}

func TestRedisKeyPrefixIsolatesStores(t *testing.T) {
	issuer := redisStore(t, "turna-a:")
	same := redisStore(t, "turna-a:")
	other := redisStore(t, "turna-b:")
	legacy := redisStore(t, "")

	id, err := issuer.CodeGen(t.Context(), Code{Alias: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := issuer.redisClient.Exists(t.Context(), "turna-a:code_"+id).Result(); n != 1 {
		t.Fatalf("prefixed raw key missing")
	}

	for name, s := range map[string]*StoreCache{"other prefix": other, "no prefix": legacy} {
		if _, ok, _ := s.TakeCode(t.Context(), "code_"+id); ok {
			t.Fatalf("%s store must not see the code", name)
		}
		if _, ok, _ := s.Code.Get(t.Context(), "code_"+id); ok {
			t.Fatalf("%s store Get must not see the code", name)
		}
	}

	raw, ok, err := same.TakeCode(t.Context(), "code_"+id)
	if err != nil || !ok {
		t.Fatalf("same prefix take = %v, %v", ok, err)
	}
	if code, _ := Decode[Code](raw); code.Alias != "u" {
		t.Fatalf("code = %+v", code)
	}
	if _, ok, _ := same.TakeCode(t.Context(), "code_"+id); ok {
		t.Fatal("code must be single use")
	}
}
