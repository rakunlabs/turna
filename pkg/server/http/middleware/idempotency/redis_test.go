package idempotency

import (
	"os"
	"strconv"
	"testing"
	"time"

	storeredis "github.com/rakunlabs/cache/store/redis"
)

// Set IDEMPOTENCY_TEST_REDIS=host:port to run against a real Redis.
func TestRedisStore(t *testing.T) {
	addr := os.Getenv("IDEMPOTENCY_TEST_REDIS")
	if addr == "" {
		t.Skip("IDEMPOTENCY_TEST_REDIS is not set")
	}

	prefix := "turna_idempotency_test_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_"

	s, err := newRedisStore(t.Context(), &Redis{ConnConfig: storeredis.ConnConfig{Address: []string{addr}}, KeyPrefix: prefix})
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()

	if ok, err := s.setNX(ctx, "k", []byte("lock-a"), time.Minute); err != nil || !ok {
		t.Fatalf("setNX = %v %v", ok, err)
	}

	if ok, _ := s.setNX(ctx, "k", []byte("lock-b"), time.Minute); ok {
		t.Fatal("second setNX succeeded")
	}

	// other holders change nothing
	if err := s.replace(ctx, "k", []byte("lock-b"), []byte("done-b"), time.Minute); err != nil {
		t.Fatal(err)
	}

	if err := s.release(ctx, "k", []byte("lock-b")); err != nil {
		t.Fatal(err)
	}

	if v, ok, _ := s.get(ctx, "k"); !ok || string(v) != "lock-a" {
		t.Fatalf("value = %q %v", v, ok)
	}

	if err := s.replace(ctx, "k", []byte("lock-a"), []byte("done-a"), time.Minute); err != nil {
		t.Fatal(err)
	}

	if v, _, _ := s.get(ctx, "k"); string(v) != "done-a" {
		t.Fatalf("value = %q", v)
	}

	if err := s.release(ctx, "k", []byte("done-a")); err != nil {
		t.Fatal(err)
	}

	if _, ok, _ := s.get(ctx, "k"); ok {
		t.Fatal("not released")
	}

	// the full middleware through Redis
	u := &upstream{}
	h := setup(t, &Idempotency{Redis: &Redis{ConnConfig: storeredis.ConnConfig{Address: []string{addr}}, KeyPrefix: prefix}}, u)

	post(h, "m", "x")
	if rec := post(h, "m", "x"); rec.Body.String() != "order 1" || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay %q %v", rec.Body.String(), rec.Header())
	}
}
