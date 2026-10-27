package faultinjection

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func setup(t *testing.T, m *FaultInjection) http.Handler {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

func request(h http.Handler, path string, headers ...string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Code
}

func TestAbort(t *testing.T) {
	h := setup(t, &FaultInjection{Abort: &Abort{Percent: 100, StatusCode: 500}})
	if code := request(h, "/"); code != http.StatusInternalServerError {
		t.Fatalf("status = %d", code)
	}

	h = setup(t, &FaultInjection{Abort: &Abort{Percent: 0}})
	if code := request(h, "/"); code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}
}

func TestAbortShare(t *testing.T) {
	h := setup(t, &FaultInjection{Abort: &Abort{Percent: 30}})

	aborted := 0
	for range 2000 {
		if request(h, "/") == http.StatusServiceUnavailable {
			aborted++
		}
	}

	if aborted < 450 || aborted > 750 {
		t.Fatalf("aborted %d of 2000", aborted)
	}
}

func TestDelay(t *testing.T) {
	h := setup(t, &FaultInjection{Delay: &Delay{Duration: 30 * time.Millisecond, Percent: 100}})

	start := time.Now()
	if code := request(h, "/"); code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}

	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("not delayed")
	}
}

func TestHeaderAndPaths(t *testing.T) {
	h := setup(t, &FaultInjection{Abort: &Abort{Percent: 100}, Header: "X-Fault", Paths: []string{"/api"}})

	cases := []struct {
		path    string
		headers []string
		want    int
	}{
		{"/api/x", nil, http.StatusNoContent},
		{"/api/x", []string{"X-Fault", "true"}, http.StatusServiceUnavailable},
		{"/api/x", []string{"X-Fault", "false"}, http.StatusNoContent},
		{"/api/x", []string{"X-Fault", "0"}, http.StatusNoContent},
		{"/other", []string{"X-Fault", "true"}, http.StatusNoContent},
	}

	for _, c := range cases {
		if code := request(h, c.path, c.headers...); code != c.want {
			t.Errorf("%s %v: status = %d, want %d", c.path, c.headers, code, c.want)
		}
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*FaultInjection{
		{},
		{Delay: &Delay{Percent: 10}},
		{Delay: &Delay{Duration: time.Second, Percent: 101}},
		{Abort: &Abort{Percent: -1}},
		{Abort: &Abort{Percent: 1, StatusCode: 99}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
