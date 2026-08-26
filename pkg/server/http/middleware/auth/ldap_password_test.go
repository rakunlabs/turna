package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestUnknownPasswordGrantAuthenticatesLDAPBeforeSync(t *testing.T) {
	t.Parallel()

	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		OAuthClients: map[string]AccessClient{
			"client": {ClientSecret: "secret"},
		},
		LDAP: []LDAPSettings{{
			Addr:       "ldap://%",
			UserBaseDN: "ou=people,dc=example,dc=com",
		}},
		TOTP: TOTPSettings{Disabled: true},
	})
	m := &Auth{cache: cache}

	form := url.Values{
		"grant_type":    {"password"},
		"client_id":     {"client"},
		"client_secret": {"secret"},
		"username":      {"unknown"},
		"password":      {"wrong"},
	}
	r := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	m.APIToken(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "failed connecting to LDAP server") || strings.Contains(body, "ldap connection problem") {
		t.Fatalf("password grant did not attempt LDAP bind before sync: %s", body)
	}
}
