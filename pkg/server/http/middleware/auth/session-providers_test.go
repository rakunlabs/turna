package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

func TestSessionProviders(t *testing.T) {
	c := NewCache(nil)
	c.snap.Store(&Snapshot{
		Version: 7,
		SessionProviders: map[string]session.Provider{
			"turna": {AuthMiddleware: "auth", Oauth2: &session.Oauth2{ClientID: "ui"}},
		},
	})

	m := &Auth{cache: c}

	providers, version := m.SessionProviders()
	if version != 7 {
		t.Fatalf("version = %d, want 7", version)
	}
	if providers["turna"].Oauth2.ClientID != "ui" {
		t.Fatalf("providers = %+v", providers)
	}

	// the middleware satisfies the session interface
	var _ session.InfSessionProviders = m
}

func TestSessionProvidersAPI(t *testing.T) {
	c := NewCache(nil)
	c.snap.Store(&Snapshot{
		Version: 3,
		SessionProviders: map[string]session.Provider{
			"turna": {Name: "Turna", AuthMiddleware: "auth"},
		},
	})

	m := &Auth{cache: c}

	w := httptest.NewRecorder()
	m.SessionProvidersAPI(w, httptest.NewRequest(http.MethodGet, "/v1/session-providers", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp struct {
		Payload map[string]session.Provider `json:"payload"`
		Meta    struct {
			Version uint64 `json:"version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Meta.Version != 3 {
		t.Fatalf("meta.version = %d, want 3", resp.Meta.Version)
	}
	if resp.Payload["turna"].Name != "Turna" {
		t.Fatalf("payload = %+v", resp.Payload)
	}
	if resp.Payload["turna"].AuthMiddleware != "auth" {
		t.Fatalf("payload = %+v", resp.Payload)
	}
}

func TestSessionProviderSettingsDecode(t *testing.T) {
	raw := []byte(`{
		"providers": {
			"turna": {
				"name": "Turna",
				"auth_middleware": "auth",
				"passkey": true,
				"password_flow": true,
				"priority": 2,
				"oauth2": {"client_id": "ui", "scopes": ["openid"]}
			}
		}
	}`)

	var setting SessionProviderSettings
	if err := json.Unmarshal(raw, &setting); err != nil {
		t.Fatalf("decode: %v", err)
	}

	provider := setting.Providers["turna"]
	if provider.Name != "Turna" || provider.AuthMiddleware != "auth" {
		t.Fatalf("provider = %+v", provider)
	}
	if !provider.Passkey || !provider.PasswordFlow || provider.Priority != 2 {
		t.Fatalf("provider = %+v", provider)
	}
	if provider.Oauth2 == nil || provider.Oauth2.ClientID != "ui" {
		t.Fatalf("oauth2 = %+v", provider.Oauth2)
	}
}
