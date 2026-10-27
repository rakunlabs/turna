package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/ldap"
)

func TestURIPrefixMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target, allow string
		want          bool
	}{
		{"https://app.example.com/cb", "https://app.example.com", true},
		{"https://app.example.com/cb", "https://app.example.com/", true},
		{"https://APP.example.com/cb", "https://app.example.com/", true},
		{"https://app.example.com/cb", "https://app.example.com/cb", true},
		{"https://app.example.com/cb/x?y=1", "https://app.example.com/cb", true},
		{"https://app.example.com/cb?y=1", "https://app.example.com/cb", true},
		{"http://127.0.0.1:53111/cb", "http://127.0.0.1/", true},
		{"com.example.app:/oauth/cb", "com.example.app:/oauth", true},

		{"https://app.example.com.evil.net/cb", "https://app.example.com", false},
		{"https://app.example.com@evil.net/cb", "https://app.example.com", false},
		{"https://user@app.example.com/cb", "https://app.example.com", false},
		{"https://app.example.com/cb-evil", "https://app.example.com/cb", false},
		{"http://app.example.com/cb", "https://app.example.com/", false},
		{"https://app.example.com:8443/cb", "https://app.example.com:443/", false},
		{"evil.app:/oauth/cb", "com.example.app:/oauth", false},
		{"https://app.example.com/cb", "", false},
	}

	for _, tt := range tests {
		if got := uriPrefixMatch(tt.target, tt.allow); got != tt.want {
			t.Errorf("uriPrefixMatch(%q, %q) = %v, want %v", tt.target, tt.allow, got, tt.want)
		}
	}
}

func TestInteractiveOnlyRejectsAPIKeys(t *testing.T) {
	t.Parallel()

	m := &Auth{}
	handler := m.interactiveOnly(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for principal, want := range map[string]int{
		"alice":                     http.StatusOK,
		apiKeyPrincipalPrefix + "x": http.StatusForbidden,
	} {
		r := httptest.NewRequest(http.MethodPost, "/v1/totp/register", nil)
		r.Header.Set("X-User", principal)
		w := httptest.NewRecorder()
		handler(w, r)

		if w.Code != want {
			t.Errorf("principal %q: status = %d, want %d", principal, w.Code, want)
		}
	}
}

func TestLDAPIndexSkipsLocalUsers(t *testing.T) {
	t.Parallel()

	// a self-signup account holding a directory user's email must not be
	// bound to the LDAP entry (and so receive its group roles)
	local := &data.User{ID: "local", Local: true, Alias: []string{"jdoe@example.com"},
		Details: map[string]any{ldapDNKey: "uid=jdoe,dc=example"}}
	index := newLDAPUserIndex(ldapTestSnapshot(local))

	if got := index.find("jdoe", &ldap.LdapUser{DN: "uid=jdoe,dc=example", UID: "jdoe", Email: "jdoe@example.com"}); got != nil {
		t.Fatalf("find() = %q, want no match for a local user", got.ID)
	}
}

func TestPasswordLengthError(t *testing.T) {
	t.Parallel()

	cfg := SignupSettings{PasswordMinLength: 8}
	if msg := cfg.passwordLengthError("short"); msg == "" {
		t.Error("short password accepted")
	}
	if msg := cfg.passwordLengthError(strings.Repeat("a", maxPasswordBytes+1)); msg == "" {
		t.Error("password over the bcrypt limit accepted")
	}
	if msg := cfg.passwordLengthError("long-enough"); msg != "" {
		t.Errorf("valid password rejected: %s", msg)
	}
}

func TestHashUserPasswordFailsClosed(t *testing.T) {
	t.Parallel()

	details := map[string]any{"password": strings.Repeat("a", maxPasswordBytes+1)}
	if err := hashUserPassword(details); err == nil {
		t.Fatal("expected an error for a password bcrypt cannot hash")
	}
}

func TestExchangeScope(t *testing.T) {
	t.Parallel()

	subject := []string{"openid", "profile", "api"}

	if got, ok := exchangeScope(nil, subject); !ok || len(got) != 3 {
		t.Errorf("empty request should inherit the subject scope, got %v %v", got, ok)
	}
	if got, ok := exchangeScope([]string{"api"}, subject); !ok || len(got) != 1 || got[0] != "api" {
		t.Errorf("narrowing should be allowed, got %v %v", got, ok)
	}
	if _, ok := exchangeScope([]string{"api", "admin"}, subject); ok {
		t.Error("widening the scope must be refused")
	}
	if _, ok := exchangeScope([]string{"api"}, nil); ok {
		t.Error("a scope-less subject token cannot be exchanged for scopes")
	}
}

func TestTokenExchangeClientAllowed(t *testing.T) {
	t.Parallel()

	if !(TokenExchangeSettings{}).clientAllowed("any") {
		t.Error("an empty allow-list keeps the grant open")
	}

	cfg := TokenExchangeSettings{AllowedClients: []string{"gateway"}}
	if !cfg.clientAllowed("gateway") || cfg.clientAllowed("other") {
		t.Error("allow-list not applied")
	}
}

func TestFederatedSubject(t *testing.T) {
	t.Parallel()

	claims := map[string]any{"sub": "abc", "id": float64(42), "oid": []any{"x"}}

	if got := federatedSubject(claims, ClaimMapping{}, "sub"); got != "abc" {
		t.Errorf("default claim: %q", got)
	}
	if got := federatedSubject(claims, ClaimMapping{SubjectClaim: "id"}, "sub"); got != "42" {
		t.Errorf("numeric claim: %q", got)
	}
	if got := federatedSubject(claims, ClaimMapping{SubjectClaim: "oid"}, "sub"); got != "x" {
		t.Errorf("list claim: %q", got)
	}
	if got := federatedSubject(claims, ClaimMapping{SubjectClaim: "-"}, "sub"); got != "" {
		t.Errorf("linking off: %q", got)
	}
	if got := federatedSubject(map[string]any{"name_id": "t"}, ClaimMapping{}, "persistent_name_id"); got != "" {
		t.Errorf("a non-persistent SAML NameID must not link: %q", got)
	}
}

func TestFederatedAttachable(t *testing.T) {
	t.Parallel()

	if _, err := federatedAttachable(&data.User{ServiceAccount: true}, ClaimMapping{LinkLocalUsers: true}); err == nil {
		t.Error("service accounts must never be attached")
	}
	if _, err := federatedAttachable(&data.User{Local: true}, ClaimMapping{}); err == nil {
		t.Error("local accounts need link_local_users")
	}
	if _, err := federatedAttachable(&data.User{Local: true}, ClaimMapping{LinkLocalUsers: true}); err != nil {
		t.Errorf("link_local_users should allow a local account: %v", err)
	}
	if _, err := federatedAttachable(&data.User{}, ClaimMapping{}); err != nil {
		t.Errorf("non-local users attach by alias: %v", err)
	}
}

func TestClaimTrue(t *testing.T) {
	t.Parallel()

	for v, want := range map[any]bool{true: true, false: false, "true": true, "TRUE": true, "false": false, nil: false} {
		if claimTrue(v) != want {
			t.Errorf("claimTrue(%v) != %v", v, want)
		}
	}
}
