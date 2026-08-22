package login

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

func TestMethodsEndpointAndLegacyInfoAlias(t *testing.T) {
	const sessionName = "login-methods-test"
	session.GlobalRegistry.Set(sessionName, &session.Session{
		Provider: map[string]session.Provider{},
	})

	m := &Login{
		Path:              Path{Base: "/login/"},
		SessionMiddleware: sessionName,
		Info:              Info{Title: "Test Login"},
	}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	middleware, err := m.Middleware(ctx)
	if err != nil {
		t.Fatalf("Middleware: %v", err)
	}
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("next called for %s", r.URL.Path)
		http.NotFound(w, r)
	}))

	for _, tt := range []struct {
		path    string
		wrapped bool
	}{
		{path: "/login/auth/methods", wrapped: true},
		{path: "/login/auth/methods/", wrapped: true},
		{path: "/login/auth/info/ui", wrapped: false},
	} {
		t.Run(tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}

			var response InfoUIResponse
			if tt.wrapped {
				var envelope MethodsResponse
				if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
					t.Fatalf("decode methods response: %v", err)
				}
				response = envelope.Payload
			} else if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("decode legacy response: %v", err)
			}
			if response.Title != "Test Login" {
				t.Fatalf("title = %q, want Test Login", response.Title)
			}
		})
	}
}

func TestCustomMethodsPath(t *testing.T) {
	m := &Login{
		Path:              Path{Base: "/login/", Methods: "/sign-in/options"},
		SessionMiddleware: "unused",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Middleware setup only needs the already-resolved session for this public
	// endpoint; the custom route is served before any session lookup.
	m.session = &session.Session{Provider: map[string]session.Provider{}}
	middleware, err := m.Middleware(ctx)
	if err != nil {
		t.Fatalf("Middleware: %v", err)
	}

	w := httptest.NewRecorder()
	middleware(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sign-in/options", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var response MethodsResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Payload.Title != "Login" {
		t.Fatalf("title = %q, want Login", response.Payload.Title)
	}
}
