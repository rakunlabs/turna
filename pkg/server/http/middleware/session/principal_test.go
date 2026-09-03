package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

type fakeAPIKeyIssuer struct {
	fakeIssuer
	body []byte
}

func (f *fakeAPIKeyIssuer) APIKeyData(context.Context, string) ([]byte, error) {
	return f.body, nil
}

func TestRequestPrincipal(t *testing.T) {
	t.Run("ordinary identity uses X-User", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "user@example.com")

		if got := RequestPrincipal(r); got != "user@example.com" {
			t.Fatalf("RequestPrincipal() = %q", got)
		}
	})

	t.Run("api key uses canonical subject", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "robot@example.com")
		turna, r := tcontext.New(w, r)
		turna.Set("claims", &claims.Custom{Map: map[string]any{
			"principal_type": "api_key",
			"api_key_id":     "key-1",
			"sub":            "api-key:key-1",
		}})
		turna.Set(apiKeyAuthenticatedContextKey, true)

		if got := RequestPrincipal(r); got != "api-key:key-1" {
			t.Fatalf("RequestPrincipal() = %q", got)
		}
	})

	t.Run("invalid api key subject is rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "user@example.com")
		turna, r := tcontext.New(w, r)
		turna.Set("claims", &claims.Custom{Map: map[string]any{
			"principal_type": "api_key",
			"api_key_id":     "key-1",
			"sub":            "api-key:other",
		}})
		turna.Set(apiKeyAuthenticatedContextKey, true)

		if got := RequestPrincipal(r); got != "" {
			t.Fatalf("RequestPrincipal() = %q, want empty", got)
		}
	})

	t.Run("api key shaped regular token uses header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-User", "regular-user")
		turna, r := tcontext.New(httptest.NewRecorder(), r)
		turna.Set("claims", &claims.Custom{Map: map[string]any{
			"principal_type": "api_key",
			"api_key_id":     "key-1",
			"sub":            "api-key:key-1",
		}})

		if got := RequestPrincipal(r); got != "regular-user" {
			t.Fatalf("RequestPrincipal() = %q", got)
		}
	})
}

func TestAPIKeyFriendlyHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	customClaims := &claims.Custom{Map: map[string]any{
		"email":              "robot@example.com",
		"preferred_username": "deploy-key",
		"name":               "deploy-key",
	}}

	addXUserHeader(r, customClaims, nil, false, nil)

	if got := r.Header.Get("X-User"); got != "robot@example.com" {
		t.Fatalf("X-User = %q", got)
	}
	if got := r.Header.Get("X-User-Id"); got != "deploy-key" {
		t.Fatalf("X-User-Id = %q", got)
	}
}

func TestServeAPIKeyMarksCanonicalPrincipal(t *testing.T) {
	IssuerRegistry.Set("principal-api-key", &fakeAPIKeyIssuer{body: []byte(`{
		"sub":"api-key:key-1",
		"principal_type":"api_key",
		"api_key_id":"key-1",
		"preferred_username":"deploy-key",
		"email":"robot@example.com"
	}`)})

	m := &Session{}
	provider := Provider{AuthMiddleware: "principal-api-key"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-API-Key", "secret")
	_, r = tcontext.New(w, r)

	var principal string
	m.serveAPIKey(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal = RequestPrincipal(r)
		w.WriteHeader(http.StatusNoContent)
	}), w, r, "turna", provider, "X-API-Key", "secret")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
	if principal != "api-key:key-1" {
		t.Fatalf("principal = %q", principal)
	}
	if got := r.Header.Get("X-User"); got != "robot@example.com" {
		t.Fatalf("X-User = %q", got)
	}
	if got := r.Header.Get("X-API-Key"); got != "" {
		t.Fatalf("X-API-Key was not removed")
	}
}
