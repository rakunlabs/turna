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
		},
		"groups": {
			"internal": {
				"providers": {
					"keycloak": {"name": "Keycloak", "oauth2": {"client_id": "kc"}}
				}
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

	grouped := setting.Groups["internal"].Providers["keycloak"]
	if grouped.Name != "Keycloak" || grouped.Oauth2 == nil || grouped.Oauth2.ClientID != "kc" {
		t.Fatalf("grouped provider = %+v", grouped)
	}
}

func TestSessionProvidersGroup(t *testing.T) {
	c := NewCache(nil)
	c.snap.Store(&Snapshot{
		Version: 5,
		SessionProviders: map[string]session.Provider{
			"turna":    {Name: "Turna"},
			"keycloak": {Name: "Keycloak"},
		},
		SessionProviderGroups: map[string]map[string]session.Provider{
			"internal": {"keycloak": {Name: "Keycloak"}},
		},
	})

	m := &Auth{cache: c}

	providers, version, ok := m.SessionProvidersGroup("internal")
	if !ok || version != 5 {
		t.Fatalf("ok = %v, version = %d", ok, version)
	}
	if len(providers) != 1 || providers["keycloak"].Name != "Keycloak" {
		t.Fatalf("providers = %+v", providers)
	}

	if _, _, ok := m.SessionProvidersGroup("missing"); ok {
		t.Fatal("unknown group must not be found")
	}

	// the middleware satisfies the session group interface
	var _ session.InfSessionProviderGroups = m
}

func TestSessionProvidersGroupAPI(t *testing.T) {
	c := NewCache(nil)
	c.snap.Store(&Snapshot{
		Version: 9,
		SessionProviderGroups: map[string]map[string]session.Provider{
			"internal": {"keycloak": {Name: "Keycloak", AuthMiddleware: "auth"}},
		},
	})

	m := &Auth{cache: c}

	r := httptest.NewRequest(http.MethodGet, "/v1/session-providers/internal", nil)
	r.SetPathValue("group", "internal")
	w := httptest.NewRecorder()
	m.SessionProvidersGroupAPI(w, r)

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

	if resp.Meta.Version != 9 {
		t.Fatalf("meta.version = %d, want 9", resp.Meta.Version)
	}
	if len(resp.Payload) != 1 || resp.Payload["keycloak"].Name != "Keycloak" {
		t.Fatalf("payload = %+v", resp.Payload)
	}

	// unknown group answers 404
	r = httptest.NewRequest(http.MethodGet, "/v1/session-providers/missing", nil)
	r.SetPathValue("group", "missing")
	w = httptest.NewRecorder()
	m.SessionProvidersGroupAPI(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestValidateSessionProviders(t *testing.T) {
	valid := SessionProviderSettings{
		Providers: map[string]session.Provider{"turna": {}},
		Groups: map[string]SessionProviderGroup{
			"internal": {Providers: map[string]session.Provider{"keycloak": {}}},
			"external": {Providers: map[string]session.Provider{"github": {}}},
		},
	}
	if err := validateSessionProviders(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	dupWithUngrouped := SessionProviderSettings{
		Providers: map[string]session.Provider{"turna": {}},
		Groups: map[string]SessionProviderGroup{
			"internal": {Providers: map[string]session.Provider{"turna": {}}},
		},
	}
	if err := validateSessionProviders(dupWithUngrouped); err == nil {
		t.Fatal("duplicate key between ungrouped and a group must be rejected")
	}

	dupAcrossGroups := SessionProviderSettings{
		Groups: map[string]SessionProviderGroup{
			"internal": {Providers: map[string]session.Provider{"keycloak": {}}},
			"external": {Providers: map[string]session.Provider{"keycloak": {}}},
		},
	}
	if err := validateSessionProviders(dupAcrossGroups); err == nil {
		t.Fatal("duplicate key across groups must be rejected")
	}

	for _, name := range []string{"", " ", "a/b", "a b", "a?b", "a#b", "a%b"} {
		bad := SessionProviderSettings{
			Groups: map[string]SessionProviderGroup{
				name: {Providers: map[string]session.Provider{"x": {}}},
			},
		}
		if err := validateSessionProviders(bad); err == nil {
			t.Fatalf("group name %q must be rejected", name)
		}
	}
}
