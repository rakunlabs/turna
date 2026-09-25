package store

import (
	"os"
	"testing"

	redis "github.com/redis/go-redis/v9"
	"github.com/worldline-go/conn/connredis"
)

func TestRedisClusterCanBeForcedWithOneAddress(t *testing.T) {
	client, err := newRedisClient(t.Context(), connredis.Config{Address: []string{"redis:6379"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, ok := client.(*redis.ClusterClient); !ok {
		t.Fatalf("client = %T, want *redis.ClusterClient", client)
	}
}

func TestRedisClusterEnabled(t *testing.T) {
	tests := []struct {
		name string
		info string
		want bool
	}{
		{name: "cluster", info: "# Cluster\r\ncluster_enabled:1\r\n", want: true},
		{name: "standalone", info: "# Cluster\r\ncluster_enabled:0\r\n"},
		{name: "empty"},
		{name: "unrelated one", info: "cluster_state:1\ncluster_enabled:0\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redisClusterEnabled(tt.info); got != tt.want {
				t.Fatalf("redisClusterEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
