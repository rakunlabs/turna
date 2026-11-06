package reload

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/reloader"
)

func serve(t *testing.T, m *Reload, method string) *httptest.ResponseRecorder {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mw(nil).ServeHTTP(rec, httptest.NewRequest(method, "/-/reload", nil))

	return rec
}

func TestReload(t *testing.T) {
	t.Cleanup(func() { reloader.Set(nil) })

	if rec := serve(t, &Reload{}, http.MethodPost); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without reloader status = %d", rec.Code)
	}

	var calls int
	reloader.Set(func(_ context.Context, reason string) error {
		if reason != "http" {
			t.Fatalf("reason = %q", reason)
		}

		calls++

		return nil
	})

	if rec := serve(t, &Reload{}, http.MethodPost); rec.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status = %d, calls = %d", rec.Code, calls)
	}

	rec := serve(t, &Reload{}, http.MethodGet)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost || calls != 1 {
		t.Fatalf("GET status = %d, allow = %q, calls = %d", rec.Code, rec.Header().Get("Allow"), calls)
	}

	if rec := serve(t, &Reload{Methods: []string{"get", "put"}}, http.MethodGet); rec.Code != http.StatusOK || calls != 2 {
		t.Fatalf("custom method status = %d", rec.Code)
	}

	reloader.Set(func(context.Context, string) error { return errors.New("bad config") })

	if rec := serve(t, &Reload{}, http.MethodPost); rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed reload status = %d", rec.Code)
	}

	if _, err := (&Reload{Timeout: -1}).Middleware(); err == nil {
		t.Fatal("negative timeout accepted")
	}
}

func TestConfigUnchanged(t *testing.T) {
	m := &Reload{Methods: []string{" post", "put "}}
	before, _ := json.Marshal(m)

	if _, err := m.Middleware(); err != nil {
		t.Fatal(err)
	}

	if after, _ := json.Marshal(m); string(after) != string(before) {
		t.Fatalf("config changed by Middleware: %s -> %s", before, after)
	}
}
