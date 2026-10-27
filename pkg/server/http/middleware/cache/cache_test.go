package cache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type upstream struct {
	calls  atomic.Int32
	header http.Header
	status int
	fail   atomic.Bool
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := u.calls.Add(1)

	if u.fail.Load() {
		httputil.ReportUpstreamFailure(r)
		w.WriteHeader(http.StatusBadGateway)

		return
	}

	for k, v := range u.header {
		w.Header()[k] = v
	}

	status := u.status
	if status == 0 {
		status = http.StatusOK
	}

	w.WriteHeader(status)
	_, _ = w.Write([]byte("body " + strconv.Itoa(int(n)) + " " + r.Header.Get("Accept-Language")))
}

func setup(t *testing.T, m *Cache, u *upstream) http.Handler {
	t.Helper()

	mw, err := m.Middleware(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	return mw(u)
}

func get(h http.Handler, path string, headers ...string) *httptest.ResponseRecorder {
	return do(h, http.MethodGet, path, headers...)
}

func do(h http.Handler, method, path string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestHitMiss(t *testing.T) {
	u := &upstream{header: http.Header{"Cache-Control": {"max-age=60"}, "Etag": {`"v1"`}}}
	h := setup(t, &Cache{}, u)

	first := get(h, "/a")
	if first.Header().Get("X-Cache") != "MISS" || first.Body.String() != "body 1 " {
		t.Fatalf("first: %s %q", first.Header().Get("X-Cache"), first.Body.String())
	}

	second := get(h, "/a")
	if second.Header().Get("X-Cache") != "HIT" || second.Body.String() != "body 1 " {
		t.Fatalf("second: %s %q", second.Header().Get("X-Cache"), second.Body.String())
	}

	if second.Header().Get("Age") == "" || second.Header().Get("Etag") != `"v1"` {
		t.Fatalf("headers %v", second.Header())
	}

	if get(h, "/b").Header().Get("X-Cache") != "MISS" {
		t.Fatal("other path hit")
	}

	if rec := get(h, "/a", "If-None-Match", `"v1"`); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("conditional: %d %q", rec.Code, rec.Body.String())
	}

	if rec := do(h, http.MethodHead, "/a"); rec.Header().Get("X-Cache") != "HIT" || rec.Body.Len() != 0 {
		t.Fatalf("head: %s %q", rec.Header().Get("X-Cache"), rec.Body.String())
	}

	if u.calls.Load() != 2 {
		t.Fatalf("upstream calls = %d", u.calls.Load())
	}
}

func TestNotStored(t *testing.T) {
	tests := []struct {
		name    string
		header  http.Header
		status  int
		request []string
		cache   Cache
	}{
		{name: "no lifetime", header: http.Header{}},
		{name: "no-store", header: http.Header{"Cache-Control": {"no-store, max-age=60"}}},
		{name: "private", header: http.Header{"Cache-Control": {"private, max-age=60"}}},
		{name: "set-cookie", header: http.Header{"Cache-Control": {"max-age=60"}, "Set-Cookie": {"a=b"}}},
		{name: "vary star", header: http.Header{"Cache-Control": {"max-age=60"}, "Vary": {"*"}}},
		{name: "status", header: http.Header{"Cache-Control": {"max-age=60"}}, status: 500},
		{name: "authorization", header: http.Header{"Cache-Control": {"max-age=60"}}, request: []string{"Authorization", "Bearer x"}},
		{name: "too large", header: http.Header{"Cache-Control": {"max-age=60"}}, cache: Cache{MaxBodySize: "3B"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &upstream{header: tt.header, status: tt.status}
			h := setup(t, &tt.cache, u)

			get(h, "/", tt.request...)
			get(h, "/", tt.request...)

			if u.calls.Load() != 2 {
				t.Fatalf("response was stored")
			}
		})
	}
}

func TestAuthorizationPublic(t *testing.T) {
	u := &upstream{header: http.Header{"Cache-Control": {"public, max-age=60"}}}
	h := setup(t, &Cache{}, u)

	get(h, "/", "Authorization", "Bearer x")
	if get(h, "/", "Authorization", "Bearer x").Header().Get("X-Cache") != "HIT" {
		t.Fatal("public response not stored")
	}
}

func TestDefaultTTL(t *testing.T) {
	u := &upstream{}
	h := setup(t, &Cache{TTL: time.Minute}, u)

	get(h, "/")
	if get(h, "/").Header().Get("X-Cache") != "HIT" {
		t.Fatal("ttl not applied")
	}
}

func TestVary(t *testing.T) {
	u := &upstream{header: http.Header{"Cache-Control": {"max-age=60"}, "Vary": {"Accept-Language"}}}
	h := setup(t, &Cache{}, u)

	get(h, "/", "Accept-Language", "en")
	get(h, "/", "Accept-Language", "tr")

	if rec := get(h, "/", "Accept-Language", "en"); rec.Body.String() != "body 1 en" {
		t.Fatalf("en: %q", rec.Body.String())
	}

	if rec := get(h, "/", "Accept-Language", "tr"); rec.Body.String() != "body 2 tr" {
		t.Fatalf("tr: %q", rec.Body.String())
	}
}

func TestRequestNoCache(t *testing.T) {
	u := &upstream{header: http.Header{"Cache-Control": {"max-age=60"}}}
	h := setup(t, &Cache{}, u)

	get(h, "/")
	if rec := get(h, "/", "Cache-Control", "no-cache"); rec.Body.String() != "body 2 " {
		t.Fatalf("no-cache served %q", rec.Body.String())
	}

	// the refreshed response is stored
	if rec := get(h, "/"); rec.Body.String() != "body 2 " {
		t.Fatalf("got %q", rec.Body.String())
	}
}

func TestInvalidate(t *testing.T) {
	u := &upstream{header: http.Header{"Cache-Control": {"max-age=60"}}}
	h := setup(t, &Cache{}, u)

	get(h, "/item")
	do(h, http.MethodPost, "/item")

	if get(h, "/item").Header().Get("X-Cache") != "MISS" {
		t.Fatal("not invalidated")
	}
}

func TestStale(t *testing.T) {
	u := &upstream{header: http.Header{"Cache-Control": {"max-age=1, stale-while-revalidate=60"}}}
	mw, err := (&Cache{}).Middleware(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	h := mw(u)
	get(h, "/")

	time.Sleep(1100 * time.Millisecond)

	if rec := get(h, "/"); rec.Header().Get("X-Cache") != "STALE" || rec.Body.String() != "body 1 " {
		t.Fatalf("got %s %q", rec.Header().Get("X-Cache"), rec.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for get(h, "/").Body.String() != "body 2 " {
		if time.Now().After(deadline) {
			t.Fatal("not revalidated")
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestStaleIfError(t *testing.T) {
	u := &upstream{header: http.Header{"Cache-Control": {"max-age=1, stale-if-error=60"}}}
	h := setup(t, &Cache{}, u)

	get(h, "/")
	time.Sleep(1100 * time.Millisecond)

	u.fail.Store(true)

	rec := get(h, "/")
	if rec.Code != http.StatusOK || rec.Header().Get("X-Cache") != "STALE" {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("X-Cache"))
	}

	u.fail.Store(false)

	if rec := get(h, "/"); rec.Header().Get("X-Cache") != "MISS" || rec.Body.String() != "body 3 " {
		t.Fatalf("got %s %q", rec.Header().Get("X-Cache"), rec.Body.String())
	}
}

func TestMemoryStoreEviction(t *testing.T) {
	s := newMemoryStore(20)
	ctx := context.Background()

	_ = s.set(ctx, "a", []byte("0123456789"), time.Minute)
	_ = s.set(ctx, "b", []byte("0123456789"), time.Minute)

	if _, ok, _ := s.get(ctx, "a"); ok {
		t.Fatal("a not evicted")
	}

	if _, ok, _ := s.get(ctx, "b"); !ok {
		t.Fatal("b evicted")
	}

	now := time.Now()
	s.now = func() time.Time { return now.Add(2 * time.Minute) }

	if _, ok, _ := s.get(ctx, "b"); ok {
		t.Fatal("expired entry served")
	}

	if s.size != 0 || s.lru.Len() != 0 {
		t.Fatalf("size = %d, len = %d", s.size, s.lru.Len())
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*Cache{
		{TTL: -time.Second},
		{Methods: []string{"POST"}},
		{Statuses: []int{206}},
		{MaxBodySize: "nope"},
		{MaxSize: "0"},
	} {
		if _, err := m.Middleware(context.Background()); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
