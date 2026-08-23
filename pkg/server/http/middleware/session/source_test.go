package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeProviderIssuer is an in-process issuer with a runtime-managed session
// provider list, like the auth middleware.
type fakeProviderIssuer struct {
	kid string
	key any

	providers atomic.Pointer[map[string]Provider]
	version   atomic.Uint64

	publicPaths []string
}

func (f *fakeProviderIssuer) Keyfunc(token *jwt.Token) (any, error) {
	if kid, _ := token.Header["kid"].(string); kid != f.kid {
		return nil, ErrKIDNotFound
	}

	return f.key, nil
}

func (f *fakeProviderIssuer) IssueToken(_ *http.Request, form url.Values) ([]byte, int, error) {
	return []byte(`{"grant_type":"` + form.Get("grant_type") + `"}`), 200, nil
}

func (f *fakeProviderIssuer) SessionProviders() (map[string]Provider, uint64) {
	providers := f.providers.Load()
	if providers == nil {
		return nil, f.version.Load()
	}

	return *providers, f.version.Load()
}

func (f *fakeProviderIssuer) PublicPathPatterns() []string {
	return f.publicPaths
}

func (f *fakeProviderIssuer) set(providers map[string]Provider, version uint64) {
	f.providers.Store(&providers)
	f.version.Store(version)
}

func TestInitProviderSourceValidation(t *testing.T) {
	m := &Session{ProviderSource: &ProviderSource{}}
	if err := m.InitProviderSource(); err == nil {
		t.Fatal("expected error when neither auth_middleware nor url is set")
	}

	m = &Session{ProviderSource: &ProviderSource{AuthMiddleware: "a", URL: "https://x"}}
	if err := m.InitProviderSource(); err == nil {
		t.Fatal("expected error when both auth_middleware and url are set")
	}

	m = &Session{ProviderSource: &ProviderSource{AuthMiddleware: "a"}}
	if err := m.InitProviderSource(); err != nil {
		t.Fatalf("init: %v", err)
	}

	m = &Session{ProviderSource: &ProviderSource{URL: "https://x"}}
	if err := m.InitProviderSource(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if m.ProviderSource.client == nil {
		t.Fatal("expected client for url source")
	}
}

func TestProviderSourceOnlySetAction(t *testing.T) {
	m := &Session{
		ProviderSource: &ProviderSource{AuthMiddleware: "auth"},
		Action:         Action{Token: &Token{}},
	}

	if err := m.SetAction(); err != nil {
		t.Fatalf("source-only session must initialize before auth registers: %v", err)
	}
	if m.KeyFuncParser() == nil {
		t.Fatal("expected placeholder keyfunc until provider source resolves")
	}
}

func TestProviderSourceInProcess(t *testing.T) {
	issuer := &fakeProviderIssuer{
		kid:         "kid-1",
		key:         "public-key",
		publicPaths: []string{"/auth/oauth2/**"},
	}
	issuer.set(map[string]Provider{
		"turna": {AuthMiddleware: "sp-auth", Oauth2: &Oauth2{ClientID: "ui"}},
		// same name as the static provider: dynamic wins
		"static": {Name: "Overridden", AuthMiddleware: "sp-auth"},
	}, 1)

	IssuerRegistry.Set("sp-auth", issuer)

	m := &Session{
		Provider: map[string]Provider{
			"static": {Name: "Static"},
			"keep":   {Name: "Keep"},
		},
		ProviderSource: &ProviderSource{AuthMiddleware: "sp-auth"},
	}

	providers := m.Providers()
	if len(providers) != 3 {
		t.Fatalf("providers = %d, want 3", len(providers))
	}
	if providers["static"].Name != "Overridden" {
		t.Fatalf("static.Name = %q, want dynamic to win", providers["static"].Name)
	}
	if _, ok := providers["keep"]; !ok {
		t.Fatal("static-only provider lost")
	}

	// keyfunc validates through the in-process issuer and stamps the provider
	keyFunc := m.KeyFuncParser()
	if keyFunc == nil {
		t.Fatal("no keyfunc")
	}
	token := &jwt.Token{Header: map[string]any{"kid": "kid-1"}}
	key, err := keyFunc.Keyfunc(token)
	if err != nil {
		t.Fatalf("keyfunc: %v", err)
	}
	if key != "public-key" {
		t.Fatalf("key = %v", key)
	}

	// issuer public paths become skip patterns
	if !m.skipPath("/auth/oauth2/token") {
		t.Fatal("issuer public path not skipped")
	}

	// same version: the state is reused, not rebuilt
	before := m.dynamic.Load()
	_ = m.Providers()
	if m.dynamic.Load() != before {
		t.Fatal("state rebuilt without a version change")
	}

	// version bump with a changed list applies on the next access
	issuer.set(map[string]Provider{
		"turna": {AuthMiddleware: "sp-auth", Oauth2: &Oauth2{ClientID: "ui2"}},
	}, 2)

	provider, ok := m.GetProvider("turna")
	if !ok {
		t.Fatal("turna provider missing after update")
	}
	if provider.Oauth2.ClientID != "ui2" {
		t.Fatalf("client_id = %q, want ui2", provider.Oauth2.ClientID)
	}
	if _, ok := m.GetProvider("static"); !ok {
		t.Fatal("static provider must survive dynamic updates")
	}
	if got := m.Providers()["static"].Name; got != "Static" {
		t.Fatalf("static.Name = %q, want static value back after overlay removal", got)
	}
}

func TestProviderSourceURL(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		if r.Header.Get("X-API-Key") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		_ = json.NewEncoder(w).Encode(providersResponse{
			Payload: map[string]Provider{
				"remote": {Name: "Remote", Oauth2: &Oauth2{ClientID: "cid"}},
			},
			Meta: struct {
				Version uint64 `json:"version"`
			}{Version: 42},
		})
	}))
	defer server.Close()

	m := &Session{
		Provider: map[string]Provider{"local": {Name: "Local"}},
		ProviderSource: &ProviderSource{
			URL:     server.URL,
			TTL:     time.Minute,
			Headers: map[string]string{"X-API-Key": "secret"},
		},
	}
	if err := m.InitProviderSource(); err != nil {
		t.Fatalf("init: %v", err)
	}

	providers := m.Providers()
	if len(providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(providers))
	}
	if providers["remote"].Name != "Remote" {
		t.Fatalf("remote provider missing: %+v", providers)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}

	// within TTL: no refetch
	_ = m.Providers()
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (TTL not honored)", requests)
	}

	// expire the TTL: refetch happens, same version keeps the state
	st := m.dynamic.Load()
	expired := *st
	expired.fetchedAt = time.Now().Add(-2 * time.Minute)
	m.dynamic.Store(&expired)

	_ = m.Providers()
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 after TTL expiry", requests)
	}
}

func TestProviderSourceURLFailureKeepsState(t *testing.T) {
	healthy := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_ = json.NewEncoder(w).Encode(providersResponse{
			Payload: map[string]Provider{"remote": {Name: "Remote"}},
		})
	}))
	defer server.Close()

	m := &Session{
		ProviderSource: &ProviderSource{URL: server.URL, TTL: time.Minute},
	}
	if err := m.InitProviderSource(); err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, ok := m.GetProvider("remote"); !ok {
		t.Fatal("remote provider missing")
	}

	// upstream breaks: the last known list keeps serving
	healthy = false
	st := m.dynamic.Load()
	expired := *st
	expired.fetchedAt = time.Now().Add(-2 * time.Minute)
	m.dynamic.Store(&expired)

	if _, ok := m.GetProvider("remote"); !ok {
		t.Fatal("remote provider must survive a failed refresh")
	}

	// and the failure backs off: fetchedAt advanced
	if got := m.dynamic.Load().fetchedAt; !got.After(expired.fetchedAt) {
		t.Fatal("failed refresh must back off until the next TTL window")
	}
}
