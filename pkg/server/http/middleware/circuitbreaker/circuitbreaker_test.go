package circuitbreaker

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/breaker"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

func setup(t *testing.T, m *CircuitBreaker, status *atomic.Int32) http.Handler {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := int(status.Load())
		if code == 0 {
			httputil.ReportUpstreamFailure(r)
			code = http.StatusBadGateway
		}

		w.WriteHeader(code)
	}))
}

func do(h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	return rec
}

func TestMiddleware(t *testing.T) {
	var status atomic.Int32 // 0 is an unreachable upstream

	h := setup(t, &CircuitBreaker{Config: breaker.Config{MinRequests: 3, OpenDuration: time.Hour}}, &status)

	status.Store(http.StatusOK)
	for range 3 {
		if rec := do(h); rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	}

	status.Store(0)
	for range 3 {
		do(h)
	}

	status.Store(http.StatusOK)

	rec := do(h)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	if rec.Header().Get("Retry-After") != "3600" {
		t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
}

func TestFailureStatuses(t *testing.T) {
	var status atomic.Int32

	h := setup(t, &CircuitBreaker{Config: breaker.Config{MinRequests: 2}, FailureStatuses: []int{500}, StatusCode: 502}, &status)

	status.Store(http.StatusServiceUnavailable)
	do(h)
	do(h)

	if rec := do(h); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("503 counted as failure, status = %d", rec.Code)
	}

	status.Store(http.StatusInternalServerError)
	for range 4 {
		do(h)
	}

	if rec := do(h); rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want configured 502", rec.Code)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*CircuitBreaker{
		{Config: breaker.Config{FailureRatio: 1.5}},
		{SlowThreshold: -time.Second},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}

func TestAbortNotCounted(t *testing.T) {
	mw, err := (&CircuitBreaker{Config: breaker.Config{MinRequests: 1}}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	for range 3 {
		func() {
			defer func() {
				if v := recover(); v != http.ErrAbortHandler { //nolint:errorlint // sentinel
					t.Fatalf("panic = %v", v)
				}
			}()

			do(h)
		}()
	}

	ok := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	if rec := do(ok); rec.Code != http.StatusOK {
		t.Fatalf("aborted requests opened the circuit, status = %d", rec.Code)
	}
}
