package retry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// flaky answers fail for the first failures calls, then 200 with the body
// it received.
type flaky struct {
	calls    atomic.Int64
	failures int64
	fail     int
	header   http.Header
	bodies   []string
	attempts []string
}

func (f *flaky) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := f.calls.Add(1)
	b, _ := io.ReadAll(r.Body)
	f.bodies = append(f.bodies, string(b))
	f.attempts = append(f.attempts, r.Header.Get("X-Retry-Attempt"))

	if n <= f.failures {
		if f.fail == 0 {
			// unreachable upstream, like the service middleware reports it
			httputil.ReportUpstreamFailure(r)
			w.WriteHeader(http.StatusBadGateway)

			return
		}

		for k, v := range f.header {
			w.Header()[k] = v
		}

		w.Header().Set("X-Failed", "yes")
		w.WriteHeader(f.fail)
		_, _ = w.Write([]byte("failed"))

		return
	}

	_, _ = w.Write([]byte("ok " + strconv.FormatInt(n, 10)))
}

func setup(t *testing.T, m *Retry, next http.Handler) http.Handler {
	t.Helper()

	if m.InitialInterval == 0 {
		m.InitialInterval = time.Millisecond
	}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(next)
}

func do(h http.Handler, method, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestRetries(t *testing.T) {
	f := &flaky{failures: 2, fail: http.StatusServiceUnavailable}
	h := setup(t, &Retry{AttemptHeader: "X-Retry-Attempt"}, f)

	rec := do(h, http.MethodPut, "data")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok 3" || rec.Header().Get("X-Failed") != "" {
		t.Fatalf("status = %d body = %q header %v", rec.Code, rec.Body.String(), rec.Header())
	}

	if strings.Join(f.bodies, ",") != "data,data,data" || strings.Join(f.attempts, ",") != "1,2,3" {
		t.Fatalf("bodies %v attempts %v", f.bodies, f.attempts)
	}
}

func TestGivesUp(t *testing.T) {
	f := &flaky{failures: 10, fail: http.StatusServiceUnavailable}
	h := setup(t, &Retry{Attempts: new(1)}, f)

	rec := do(h, http.MethodGet, "")
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "failed" || rec.Header().Get("X-Failed") != "yes" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	if f.calls.Load() != 2 {
		t.Fatalf("calls = %d", f.calls.Load())
	}
}

func TestStatusesAndMethods(t *testing.T) {
	f := &flaky{failures: 1, fail: http.StatusInternalServerError}
	h := setup(t, &Retry{}, f)

	if rec := do(h, http.MethodGet, ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("500 retried: %d", rec.Code)
	}

	f = &flaky{failures: 1, fail: http.StatusInternalServerError}
	h = setup(t, &Retry{Statuses: []string{"500-599"}}, f)

	if rec := do(h, http.MethodGet, ""); rec.Code != http.StatusOK {
		t.Fatalf("500 not retried: %d", rec.Code)
	}

	f = &flaky{failures: 1, fail: http.StatusServiceUnavailable}
	h = setup(t, &Retry{}, f)

	if rec := do(h, http.MethodPost, ""); rec.Code != http.StatusServiceUnavailable || f.calls.Load() != 1 {
		t.Fatalf("POST retried: %d calls %d", rec.Code, f.calls.Load())
	}

	f = &flaky{failures: 1, fail: http.StatusServiceUnavailable}
	h = setup(t, &Retry{}, f)

	if rec := do(h, http.MethodPost, "", "Idempotency-Key", "k"); rec.Code != http.StatusOK {
		t.Fatalf("POST with key not retried: %d", rec.Code)
	}
}

func TestUnreachable(t *testing.T) {
	f := &flaky{failures: 1}
	h := setup(t, &Retry{Statuses: []string{"503"}}, f)

	if rec := do(h, http.MethodGet, ""); rec.Code != http.StatusOK {
		t.Fatalf("connection error not retried: %d", rec.Code)
	}
}

func TestBodyTooLarge(t *testing.T) {
	f := &flaky{failures: 1, fail: http.StatusServiceUnavailable}
	h := setup(t, &Retry{MaxBodySize: 2}, f)

	if rec := do(h, http.MethodPut, "large"); rec.Code != http.StatusServiceUnavailable || f.bodies[0] != "large" {
		t.Fatalf("status = %d bodies %v", rec.Code, f.bodies)
	}
}

func TestRetryAfter(t *testing.T) {
	f := &flaky{failures: 1, fail: http.StatusServiceUnavailable, header: http.Header{"Retry-After": {"1"}}}
	h := setup(t, &Retry{RespectRetryAfter: true, MaxInterval: 50 * time.Millisecond}, f)

	start := time.Now()
	if rec := do(h, http.MethodGet, ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	if d := time.Since(start); d < 50*time.Millisecond || d > time.Second {
		t.Fatalf("waited %s", d)
	}
}

func TestHeadersWithoutBody(t *testing.T) {
	h := setup(t, &Retry{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Only", "header")
	}))

	if rec := do(h, http.MethodGet, ""); rec.Code != http.StatusOK || rec.Header().Get("X-Only") != "header" {
		t.Fatalf("status = %d header %v", rec.Code, rec.Header())
	}
}

func TestZeroAttempts(t *testing.T) {
	f := &flaky{failures: 1, fail: http.StatusServiceUnavailable}
	h := setup(t, &Retry{Attempts: new(0)}, f)

	if rec := do(h, http.MethodGet, ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
