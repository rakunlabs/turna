package iamforwardauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type fakeChecker struct {
	allow func(alias, host, path, method string) bool
	got   data.CheckRequest
}

func (f *fakeChecker) Keyfunc(*jwt.Token) (any, error) { return nil, session.ErrKIDNotFound }

func (f *fakeChecker) IssueToken(*http.Request, url.Values) ([]byte, int, error) {
	return nil, http.StatusNotImplemented, nil
}

func (f *fakeChecker) AccessAllowed(_ context.Context, alias, host, path, method string) (bool, error) {
	f.got = data.CheckRequest{Alias: alias, Host: host, Path: path, Method: method}

	return f.allow(alias, host, path, method), nil
}

func forwarded(user, method, host, uri string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/auth/forward", nil)
	if user != "" {
		r.Header.Set("X-User", user)
	}
	if method != "" {
		r.Header.Set("X-Forwarded-Method", method)
	}
	if host != "" {
		r.Header.Set("X-Forwarded-Host", host)
	}
	if uri != "" {
		r.Header.Set("X-Forwarded-Uri", uri)
	}

	return r
}

func TestIamForwardAuthInProcess(t *testing.T) {
	checker := &fakeChecker{allow: func(alias, host, path, method string) bool {
		return alias == "alice" && host == "app.example.com" && path == "/api/items" && method == http.MethodGet
	}}
	session.IssuerRegistry.Set("fwd-auth", checker)

	m := &IamForwardAuth{AuthMiddleware: "fwd-auth", PassHeaders: []string{"X-User"}}
	mw, err := m.Middleware()
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	nextCalled := false
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true }))

	cases := map[string]struct {
		r    *http.Request
		want int
	}{
		"allowed":            {r: forwarded("alice", "GET", "app.example.com", "/api/items?page=2"), want: http.StatusOK},
		"denied":             {r: forwarded("bob", "GET", "app.example.com", "/api/items"), want: http.StatusForbidden},
		"anonymous":          {r: forwarded("", "GET", "app.example.com", "/api/items"), want: http.StatusUnauthorized},
		"missing method":     {r: forwarded("alice", "", "app.example.com", "/api/items"), want: http.StatusUnauthorized},
		"missing host":       {r: forwarded("alice", "GET", "", "/api/items"), want: http.StatusUnauthorized},
		"missing uri":        {r: forwarded("alice", "GET", "app.example.com", ""), want: http.StatusUnauthorized},
		"dot segments clean": {r: forwarded("alice", "GET", "app.example.com", "/api/x/../items"), want: http.StatusOK},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tc.r)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (check %+v)", rec.Code, tc.want, checker.got)
			}
			if tc.want == http.StatusOK && rec.Header().Get("X-User") != "alice" {
				t.Errorf("pass header X-User = %q", rec.Header().Get("X-User"))
			}
		})
	}

	if checker.got.Path != "/api/items" {
		t.Errorf("query and dot segments must be removed from the checked path, got %q", checker.got.Path)
	}
	if nextCalled {
		t.Error("forward auth must answer itself and never call next")
	}

	t.Run("issuer without access checks", func(t *testing.T) {
		session.IssuerRegistry.Set("fwd-no-check", struct{ session.InfIssuer }{})
		m := &IamForwardAuth{AuthMiddleware: "fwd-no-check"}
		mw, err := m.Middleware()
		if err != nil {
			t.Fatal(err)
		}

		rec := httptest.NewRecorder()
		mw(nil).ServeHTTP(rec, forwarded("alice", "GET", "h", "/"))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestIamForwardAuthCheckAPI(t *testing.T) {
	var got data.CheckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		if got.Path == "/broken" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"message":"upstream down"}`))

			return
		}

		_ = json.NewEncoder(w).Encode(data.CheckResponse{Allowed: got.Alias == "alice"})
	}))
	defer server.Close()

	m := &IamForwardAuth{
		CheckAPI:     server.URL,
		MethodHeader: "X-Original-Method",
		HostHeader:   "X-Original-Host",
		URIHeader:    "X-Original-URI",
	}
	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}
	h := mw(nil)

	req := func(user, uri string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", user)
		r.Header.Set("X-Original-Method", "DELETE")
		r.Header.Set("X-Original-Host", "app.example.com")
		r.Header.Set("X-Original-URI", uri)

		return r
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req("alice", "/items/1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed status = %d", rec.Code)
	}
	if got.Method != "DELETE" || got.Host != "app.example.com" || got.Path != "/items/1" || got.Alias != "alice" {
		t.Fatalf("check request = %+v", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req("bob", "/items/1"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req("alice", "/broken"))
	if rec.Code == http.StatusOK || rec.Code == http.StatusForbidden {
		t.Fatalf("check api failure must not allow or look like a denial, status = %d", rec.Code)
	}
}

func TestIamForwardAuthConfig(t *testing.T) {
	if _, err := (&IamForwardAuth{}).Middleware(); err == nil {
		t.Fatal("check_api or auth_middleware is required")
	}
}
