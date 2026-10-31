package idempotency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type upstream struct {
	calls  atomic.Int64
	status int
	block  chan struct{}
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	n := u.calls.Add(1)
	if u.block != nil {
		<-u.block
	}

	status := u.status
	if status == 0 {
		status = http.StatusCreated
	}

	w.Header().Set("X-Call", strconv.FormatInt(n, 10))
	w.WriteHeader(status)
	_, _ = w.Write([]byte("order " + strconv.FormatInt(n, 10)))
}

func setup(t *testing.T, m *Idempotency, u http.Handler) http.Handler {
	t.Helper()

	mw, err := m.Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return mw(u)
}

func post(h http.Handler, key, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}

	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestReplay(t *testing.T) {
	u := &upstream{}
	h := setup(t, &Idempotency{}, u)

	first := post(h, "k1", `{"a":1}`)
	if first.Code != http.StatusCreated || first.Body.String() != "order 1" || first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("first: %d %q", first.Code, first.Body.String())
	}

	second := post(h, "k1", `{"a":1}`)
	if second.Code != http.StatusCreated || second.Body.String() != "order 1" || second.Header().Get("X-Call") != "1" {
		t.Fatalf("second: %d %q", second.Code, second.Body.String())
	}

	if second.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("missing replay header")
	}

	if rec := post(h, "k2", `{"a":1}`); rec.Body.String() != "order 2" {
		t.Fatalf("other key: %q", rec.Body.String())
	}

	if rec := post(h, "", `{"a":1}`); rec.Body.String() != "order 3" {
		t.Fatalf("no key: %q", rec.Body.String())
	}

	if u.calls.Load() != 3 {
		t.Fatalf("calls = %d", u.calls.Load())
	}
}

func TestMismatch(t *testing.T) {
	h := setup(t, &Idempotency{}, &upstream{})

	post(h, "k", `{"a":1}`)

	if rec := post(h, "k", `{"a":2}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestScope(t *testing.T) {
	u := &upstream{}
	h := setup(t, &Idempotency{}, u)

	post(h, "k", "x", "Authorization", "Bearer alice")

	if rec := post(h, "k", "x", "Authorization", "Bearer bob"); rec.Body.String() != "order 2" {
		t.Fatalf("other user got %q", rec.Body.String())
	}

	if rec := post(h, "k", "x", "Authorization", "Bearer alice"); rec.Body.String() != "order 1" {
		t.Fatalf("same user got %q", rec.Body.String())
	}
}

func TestErrorsNotStored(t *testing.T) {
	u := &upstream{status: http.StatusServiceUnavailable}
	h := setup(t, &Idempotency{}, u)

	post(h, "k", "x")
	post(h, "k", "x")

	if u.calls.Load() != 2 {
		t.Fatalf("calls = %d", u.calls.Load())
	}

	u2 := &upstream{status: http.StatusTooManyRequests}
	h2 := setup(t, &Idempotency{}, u2)

	post(h2, "k", "x")
	post(h2, "k", "x")

	if u2.calls.Load() != 2 {
		t.Fatalf("429 calls = %d", u2.calls.Load())
	}
}

func TestInProgress(t *testing.T) {
	u := &upstream{block: make(chan struct{})}
	h := setup(t, &Idempotency{}, u)

	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- post(h, "k", "x") }()

	for u.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	if rec := post(h, "k", "x"); rec.Code != http.StatusConflict || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("concurrent status = %d", rec.Code)
	}

	close(u.block)

	if rec := <-done; rec.Code != http.StatusCreated {
		t.Fatalf("first status = %d", rec.Code)
	}

	if rec := post(h, "k", "x"); rec.Body.String() != "order 1" {
		t.Fatalf("after: %q", rec.Body.String())
	}
}

func TestClientGoneNotStored(t *testing.T) {
	calls := 0
	h := setup(t, &Idempotency{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			// what the service middleware answers when the client is gone
			w.WriteHeader(499)

			return
		}

		w.WriteHeader(http.StatusCreated)
	}))

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/orders", strings.NewReader("x"))
	req.Header.Set("Idempotency-Key", "k")
	cancel()
	h.ServeHTTP(httptest.NewRecorder(), req)

	if rec := post(h, "k", "x"); rec.Code != http.StatusCreated || calls != 2 {
		t.Fatalf("status = %d calls = %d", rec.Code, calls)
	}

	calls = 0
	h2 := setup(t, &Idempotency{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(499)
	}))

	post(h2, "k", "x")
	post(h2, "k", "x")

	if calls != 2 {
		t.Fatalf("499 stored, calls = %d", calls)
	}
}

func TestReplayHeaders(t *testing.T) {
	h := setup(t, &Idempotency{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/orders/1")
		w.Header().Set("Date", "Mon, 01 Jan 2001 00:00:00 GMT")
		w.Header().Set("X-Request-Id", "first")
		http.SetCookie(w, &http.Cookie{Name: "s", Value: "first-user"})
		w.WriteHeader(http.StatusCreated)
	}))

	post(h, "k", "x")

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader("x"))
	req.Header.Set("Idempotency-Key", "k")

	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-Id", "second")
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Location") != "/orders/1" || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("headers %v", rec.Header())
	}

	if rec.Header().Get("Set-Cookie") != "" || rec.Header().Get("Date") != "" || rec.Header().Get("X-Request-Id") != "second" {
		t.Fatalf("per request headers replayed %v", rec.Header())
	}
}

func TestHeadersWithoutBody(t *testing.T) {
	h := setup(t, &Idempotency{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/orders/1")
	}))

	post(h, "k", "x")

	if rec := post(h, "k", "x"); rec.Header().Get("Location") != "/orders/1" || rec.Code != http.StatusOK {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
}

func TestStaleLockKept(t *testing.T) {
	m := &Idempotency{LockTimeout: 20 * time.Millisecond}
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	var calls atomic.Int64

	h := setup(t, m, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			<-releaseFirst
			// not stored, so its deferred unlock runs
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			<-releaseSecond
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusCreated)
		}
	}))

	first := make(chan struct{})
	go func() { post(h, "k", "x"); close(first) }()

	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	time.Sleep(40 * time.Millisecond)

	second := make(chan *httptest.ResponseRecorder)
	go func() { second <- post(h, "k", "x") }()

	for calls.Load() < 2 {
		time.Sleep(time.Millisecond)
	}

	// the first request ends and must not unlock the second one's key
	close(releaseFirst)
	<-first

	if rec := post(h, "k", "x"); rec.Code != http.StatusConflict {
		t.Fatalf("third status = %d, lock of the running request was removed", rec.Code)
	}

	close(releaseSecond)
	if rec := <-second; rec.Code != http.StatusCreated {
		t.Fatalf("second status = %d", rec.Code)
	}

	if rec := post(h, "k", "x"); rec.Code != http.StatusCreated || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("after second status = %d", rec.Code)
	}
}

func TestRequired(t *testing.T) {
	h := setup(t, &Idempotency{Required: true}, &upstream{})

	if rec := post(h, "", "x"); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("get status = %d", rec.Code)
	}
}

func TestBodyLimit(t *testing.T) {
	h := setup(t, &Idempotency{MaxBodySize: "4B"}, &upstream{})

	if rec := post(h, "k", "too large"); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}

	// an outer body_limit
	h = setup(t, &Idempotency{}, &upstream{})
	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader("too large"))
	req.Header.Set("Idempotency-Key", "k")

	rec := httptest.NewRecorder()
	req.Body = http.MaxBytesReader(rec, req.Body, 4)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("outer limit status = %d", rec.Code)
	}
}

func TestPanicUnlocks(t *testing.T) {
	calls := 0
	h := setup(t, &Idempotency{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			panic(http.ErrAbortHandler)
		}

		w.WriteHeader(http.StatusOK)
	}))

	func() {
		defer func() { _ = recover() }()
		post(h, "k", "x")
	}()

	if rec := post(h, "k", "x"); rec.Code != http.StatusOK || calls != 2 {
		t.Fatalf("status = %d calls = %d", rec.Code, calls)
	}
}
