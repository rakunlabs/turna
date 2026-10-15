package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

// flaky answers fail for the first n requests, then 200 with the body it
// received.
func flaky(name string, n int32, fail int, calls *atomic.Int32) {
	registry.GlobalReg.AddHttpMiddleware(name, []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)

			if calls.Add(1) <= n {
				w.Header().Set("X-Failed", "true")
				w.WriteHeader(fail)
				_, _ = w.Write([]byte("failed"))

				return
			}

			w.Header().Set("X-Attempt", "ok")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		})
	}})
}

func TestRetry(t *testing.T) {
	for _, tt := range []struct {
		name      string
		retry     Retry
		failures  int32
		fail      int
		want      int
		wantCalls int32
	}{
		{"succeeds within attempts", Retry{Codes: []int{500}, Attempts: new(3)}, 2, 500, 200, 3},
		{"exceeds attempts", Retry{Codes: []int{500}, Attempts: new(3)}, 4, 500, 500, 4},
		{"code not retried", Retry{Codes: []int{500}, Attempts: new(3)}, 2, 503, 503, 1},
		{"default codes", Retry{Attempts: new(2)}, 1, 502, 200, 2},
		{"default attempts", Retry{Codes: []int{504}}, 2, 504, 504, 2},
		{"zero attempts", Retry{Codes: []int{500}, Attempts: new(0)}, 1, 500, 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			name := "retry-" + strings.ReplaceAll(tt.name, " ", "-")
			flaky(name, tt.failures, tt.fail, &calls)

			retry := tt.retry
			g := &Gateway{Rules: []Rule{{Retry: &retry, Backends: []Backend{{Weight: 1, Middlewares: []string{name}}}}}}

			rec := serve(t, g, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("payload")))

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}

			if c := calls.Load(); c != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", c, tt.wantCalls)
			}

			if tt.want == http.StatusOK {
				if rec.Body.String() != "payload" {
					t.Fatalf("body = %q, the request body must be sent again", rec.Body.String())
				}

				if rec.Header().Get("X-Failed") != "" {
					t.Fatal("headers of a discarded attempt leaked into the response")
				}
			} else if rec.Body.String() != "failed" {
				t.Fatalf("body = %q, the last response must be returned", rec.Body.String())
			}
		})
	}
}

func TestRetryBackoff(t *testing.T) {
	var calls atomic.Int32
	flaky("retry-backoff", 1, 500, &calls)

	g := &Gateway{Rules: []Rule{{
		Retry:    &Retry{Codes: []int{500}, Attempts: new(1), Backoff: 50 * time.Millisecond},
		Backends: []Backend{{Weight: 1, Middlewares: []string{"retry-backoff"}}},
	}}}

	start := time.Now()
	rec := serve(t, g, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	if d := time.Since(start); d < 50*time.Millisecond {
		t.Fatalf("retried after %v, before the backoff", d)
	}
}

// A backend timeout bounds each attempt; a timed out attempt answers 504 and
// is retried.
func TestRetryBackendTimeout(t *testing.T) {
	var calls atomic.Int32

	registry.GlobalReg.AddHttpMiddleware("retry-slow", []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				<-r.Context().Done()
				w.WriteHeader(http.StatusBadGateway)

				return
			}

			w.WriteHeader(http.StatusOK)
		})
	}})

	g := &Gateway{Rules: []Rule{{
		Retry:          &Retry{Codes: []int{504}, Attempts: new(1)},
		BackendTimeout: 20 * time.Millisecond,
		Backends:       []Backend{{Weight: 1, Middlewares: []string{"retry-slow"}}},
	}}}

	rec := serve(t, g, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status = %d calls = %d", rec.Code, calls.Load())
	}
}

// Bodies larger than the buffer limit are sent once without retries.
func TestRetryLargeBody(t *testing.T) {
	var calls atomic.Int32
	flaky("retry-large", 1, 500, &calls)

	g := &Gateway{Rules: []Rule{{
		Retry:    &Retry{Codes: []int{500}, Attempts: new(3), MaxBodySize: 4},
		Backends: []Backend{{Weight: 1, Middlewares: []string{"retry-large"}}},
	}}}

	rec := serve(t, g, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("too large")))

	if rec.Code != http.StatusInternalServerError || calls.Load() != 1 {
		t.Fatalf("status = %d calls = %d", rec.Code, calls.Load())
	}
}

// Connection errors and backend timeouts are retried even when their status
// is not in the retried codes.
func TestRetryUpstreamFailure(t *testing.T) {
	var calls atomic.Int32

	registry.GlobalReg.AddHttpMiddleware("retry-unreachable", []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				httputil.ReportUpstreamFailure(r)
				w.WriteHeader(http.StatusBadGateway)

				return
			}

			w.WriteHeader(http.StatusOK)
		})
	}})

	g := &Gateway{Rules: []Rule{{
		Retry:    &Retry{Codes: []int{500}, Attempts: new(1)},
		Backends: []Backend{{Weight: 1, Middlewares: []string{"retry-unreachable"}}},
	}}}

	rec := serve(t, g, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("connection error: status = %d calls = %d", rec.Code, calls.Load())
	}

	// a backend answering 502 itself is not a connection error
	var answered atomic.Int32
	flaky("retry-answered-502", 1, http.StatusBadGateway, &answered)

	g = &Gateway{Rules: []Rule{{
		Retry:    &Retry{Codes: []int{500}, Attempts: new(1)},
		Backends: []Backend{{Weight: 1, Middlewares: []string{"retry-answered-502"}}},
	}}}

	rec = serve(t, g, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusBadGateway || answered.Load() != 1 {
		t.Fatalf("answered 502: status = %d calls = %d", rec.Code, answered.Load())
	}
}

func TestRetryBackendTimeoutWithoutCode(t *testing.T) {
	var calls atomic.Int32

	registry.GlobalReg.AddHttpMiddleware("retry-slow-nocode", []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				<-r.Context().Done()

				return
			}

			w.WriteHeader(http.StatusOK)
		})
	}})

	g := &Gateway{Rules: []Rule{{
		Retry:          &Retry{Codes: []int{500}, Attempts: new(1)},
		BackendTimeout: 20 * time.Millisecond,
		Backends:       []Backend{{Weight: 1, Middlewares: []string{"retry-slow-nocode"}}},
	}}}

	rec := serve(t, g, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status = %d calls = %d", rec.Code, calls.Load())
	}
}
