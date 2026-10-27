package cache

import (
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	storeredis "github.com/rakunlabs/cache/store/redis"
)

// Set CACHE_TEST_REDIS=host:port to run against a real Redis.
func TestRedis(t *testing.T) {
	addr := os.Getenv("CACHE_TEST_REDIS")
	if addr == "" {
		t.Skip("CACHE_TEST_REDIS is not set")
	}

	prefix := "turna_cache_test_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_"
	m := &Cache{Redis: &Redis{ConnConfig: storeredis.ConnConfig{Address: []string{addr}}, KeyPrefix: prefix}}

	mw, err := m.Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	u := &upstream{header: http.Header{"Cache-Control": {"max-age=60"}}}
	h := mw(u)

	get(h, "/")
	if rec := get(h, "/"); rec.Header().Get("X-Cache") != "HIT" || rec.Body.String() != "body 1 " {
		t.Fatalf("got %s %q", rec.Header().Get("X-Cache"), rec.Body.String())
	}

	do(h, http.MethodDelete, "/")
	if get(h, "/").Header().Get("X-Cache") != "MISS" {
		t.Fatal("not invalidated")
	}
}
