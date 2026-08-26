package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"golang.org/x/crypto/bcrypt"
)

func TestTokenGrantsRejectDisabledPrincipal(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("user-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	disabledUser := &data.User{
		ID:       "disabled-user",
		Alias:    []string{"disabled-user"},
		Disabled: true,
		Local:    true,
		Details: map[string]any{
			"password": base64.StdEncoding.EncodeToString(passwordHash),
		},
	}
	activeUser := &data.User{
		ID:    "active-user",
		Alias: []string{"active-user"},
		Local: true,
		Details: map[string]any{
			"password": base64.StdEncoding.EncodeToString(passwordHash),
		},
	}
	disabledService := &data.User{
		ID:             "disabled-service",
		Alias:          []string{"disabled-service"},
		Disabled:       true,
		ServiceAccount: true,
		Details:        map[string]any{"secret": "service-secret"},
	}

	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		Users: map[string]*data.User{
			disabledUser.ID:    disabledUser,
			activeUser.ID:      activeUser,
			disabledService.ID: disabledService,
		},
		Alias: map[string]string{
			"disabled-user":    disabledUser.ID,
			"active-user":      activeUser.ID,
			"disabled-service": disabledService.ID,
		},
		OAuthClients: map[string]AccessClient{
			"client": {ClientSecret: "client-secret"},
		},
		TOTP:  TOTPSettings{Disabled: true},
		Cache: CacheSettings{CodeStore: CodeStoreSettings{Active: "memory"}},
	})
	m := &Auth{cache: cache}
	defer func() { _ = m.closeCodeStore() }()

	callToken := func(t *testing.T, form url.Values) {
		t.Helper()

		r := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		m.APIToken(w, r)

		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"error":"invalid_grant"`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}

	t.Run("password", func(t *testing.T) {
		callToken(t, url.Values{
			"grant_type":    {"password"},
			"client_id":     {"client"},
			"client_secret": {"client-secret"},
			"username":      {"disabled-user"},
			"password":      {"user-password"},
		})
	})

	t.Run("authorization code", func(t *testing.T) {
		codeStore, err := m.codeStoreRuntime(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		codeRaw, err := oauth2store.Encode(oauth2store.Code{
			Alias:       "disabled-user",
			ClientID:    "client",
			RedirectURI: "https://app.example.com/callback",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := codeStore.Code.Set(t.Context(), "code_disabled-user", codeRaw); err != nil {
			t.Fatal(err)
		}

		callToken(t, url.Values{
			"grant_type":    {"authorization_code"},
			"client_id":     {"client"},
			"client_secret": {"client-secret"},
			"code":          {"disabled-user"},
			"redirect_uri":  {"https://app.example.com/callback"},
		})
	})

	t.Run("client credentials", func(t *testing.T) {
		callToken(t, url.Values{
			"grant_type":    {"client_credentials"},
			"client_id":     {"disabled-service"},
			"client_secret": {"service-secret"},
		})
	})

	t.Run("disabled delegated client", func(t *testing.T) {
		form := url.Values{
			"grant_type":    {"password"},
			"client_id":     {"disabled-service"},
			"client_secret": {"service-secret"},
			"username":      {"active-user"},
			"password":      {"user-password"},
		}
		r := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		m.APIToken(w, r)

		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"error":"invalid_client"`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}
