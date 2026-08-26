package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	oauth2auth "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
)

func TestClaimValues(t *testing.T) {
	claims := map[string]any{
		"groups": []any{"dev", "ops"},
		"realm_access": map[string]any{
			"roles": []any{"admin", "user"},
		},
		"scope":  "openid profile",
		"nested": map[string]any{"deep": map[string]any{"list": []string{"x"}}},
	}

	cases := []struct {
		path string
		want []string
	}{
		{"groups", []string{"dev", "ops"}},
		{"realm_access.roles", []string{"admin", "user"}},
		{"scope", []string{"openid", "profile"}},
		{"nested.deep.list", []string{"x"}},
		{"missing", nil},
		{"realm_access.missing", nil},
		{"", nil},
	}

	for _, c := range cases {
		got := claimValues(claims, c.path)
		if len(got) != len(c.want) {
			t.Errorf("claimValues(%q) = %v, want %v", c.path, got, c.want)
			continue
		}

		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("claimValues(%q) = %v, want %v", c.path, got, c.want)
				break
			}
		}
	}
}

func TestSetClaimByPath(t *testing.T) {
	t.Run("flat roles", func(t *testing.T) {
		claims := map[string]any{}
		setClaimByPath(claims, "roles", []string{"dispute"})

		got, ok := claims["roles"].([]string)
		if !ok || len(got) != 1 || got[0] != "dispute" {
			t.Fatalf("roles = %#v", claims["roles"])
		}
	})

	t.Run("nested realm_access.roles", func(t *testing.T) {
		claims := map[string]any{}
		setClaimByPath(claims, "realm_access.roles", []string{"dispute"})

		realm, ok := claims["realm_access"].(map[string]any)
		if !ok {
			t.Fatalf("realm_access = %#v", claims["realm_access"])
		}

		got, ok := realm["roles"].([]string)
		if !ok || len(got) != 1 || got[0] != "dispute" {
			t.Fatalf("realm_access.roles = %#v", realm["roles"])
		}
	})

	t.Run("deep path keeps existing siblings", func(t *testing.T) {
		claims := map[string]any{
			"resource_access": map[string]any{
				"other": map[string]any{"roles": []string{"keep"}},
			},
		}
		setClaimByPath(claims, "resource_access.app.roles", []string{"dispute"})

		// inverse of claimValues confirms the leaf and the untouched sibling
		if got := claimValues(claims, "resource_access.app.roles"); len(got) != 1 || got[0] != "dispute" {
			t.Fatalf("resource_access.app.roles = %v", got)
		}
		if got := claimValues(claims, "resource_access.other.roles"); len(got) != 1 || got[0] != "keep" {
			t.Fatalf("sibling overwritten: %v", got)
		}
	})

	t.Run("overwrites non-map node", func(t *testing.T) {
		claims := map[string]any{"realm_access": "scalar"}
		setClaimByPath(claims, "realm_access.roles", []string{"dispute"})

		if got := claimValues(claims, "realm_access.roles"); len(got) != 1 || got[0] != "dispute" {
			t.Fatalf("realm_access.roles = %v", got)
		}
	})

	t.Run("empty path is a no-op", func(t *testing.T) {
		claims := map[string]any{"x": 1}
		setClaimByPath(claims, "", []string{"dispute"})

		if len(claims) != 1 {
			t.Fatalf("claims mutated: %#v", claims)
		}
	})
}

func TestTokenSettingsGetRolesClaim(t *testing.T) {
	if got := (TokenSettings{}).GetRolesClaim(); got != "roles" {
		t.Fatalf("default roles claim = %q, want roles", got)
	}

	if got := (TokenSettings{RolesClaim: "realm_access.roles"}).GetRolesClaim(); got != "realm_access.roles" {
		t.Fatalf("configured roles claim = %q", got)
	}
}

func TestVerifyPKCE(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if !verifyPKCE(challenge, "S256", verifier) {
		t.Error("S256 verifier should match")
	}

	if verifyPKCE(challenge, "S256", "wrong") {
		t.Error("S256 wrong verifier should not match")
	}

	if !verifyPKCE("plain-value", "plain", "plain-value") {
		t.Error("plain verifier should match")
	}

	if verifyPKCE("plain-value", "plain", "other") {
		t.Error("plain wrong verifier should not match")
	}
}

func TestPKCEParams(t *testing.T) {
	r := httptest.NewRequest("GET", "/auth?code_challenge=abc&code_challenge_method=S256", nil)
	challenge, method, err := pkceParams(r)
	if err != nil || challenge != "abc" || method != "S256" {
		t.Errorf("pkceParams = %q %q %v", challenge, method, err)
	}

	// default method is plain
	r = httptest.NewRequest("GET", "/auth?code_challenge=abc", nil)
	if _, method, _ = pkceParams(r); method != "plain" {
		t.Errorf("default method = %q, want plain", method)
	}

	// unsupported method
	r = httptest.NewRequest("GET", "/auth?code_challenge=abc&code_challenge_method=S512", nil)
	if _, _, err = pkceParams(r); err == nil {
		t.Error("unsupported method should error")
	}

	// method without challenge
	r = httptest.NewRequest("GET", "/auth?code_challenge_method=S256", nil)
	if _, _, err = pkceParams(r); err == nil {
		t.Error("method without challenge should error")
	}

	// no pkce at all
	r = httptest.NewRequest("GET", "/auth", nil)
	if challenge, method, err = pkceParams(r); challenge != "" || method != "" || err != nil {
		t.Errorf("empty pkce = %q %q %v", challenge, method, err)
	}
}

func TestFederatedLoginRequiresClientID(t *testing.T) {
	m := &Auth{}

	for name, test := range map[string]struct {
		url     string
		handler http.HandlerFunc
	}{
		"oauth": {
			url:     "/auth/oauth2/auth/upstream?response_type=code&redirect_uri=https://app.example.com/callback",
			handler: m.APIAuth,
		},
		"saml": {
			url:     "/auth/saml/upstream/login?redirect_uri=https://app.example.com/callback",
			handler: m.SAMLLogin,
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, test.url, nil)
			w := httptest.NewRecorder()

			test.handler(w, r)

			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "client_id is required") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestFederatedPublicClientRequiresPKCE(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		OAuthClients: map[string]AccessClient{
			"public-client": {RedirectURIs: []string{"https://app.example.com/callback"}, Public: true},
		},
		OAuthProviders: map[string]ProviderConfig{"upstream": {}},
	})
	m := &Auth{cache: cache}

	for name, test := range map[string]struct {
		url     string
		handler http.HandlerFunc
	}{
		"oauth": {
			url:     "/auth/oauth2/auth/upstream?response_type=code&client_id=public-client&redirect_uri=https://app.example.com/callback",
			handler: m.APIAuth,
		},
		"saml": {
			url:     "/auth/saml/upstream/login?client_id=public-client&redirect_uri=https://app.example.com/callback",
			handler: m.SAMLLogin,
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, test.url, nil)
			r.SetPathValue("provider", "upstream")
			w := httptest.NewRecorder()

			test.handler(w, r)

			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "public clients require PKCE") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestFederatedClientAllowsRedirectWithoutRestrictions(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{OAuthClients: map[string]AccessClient{"public-client": {Public: true}}})
	m := &Auth{cache: cache}

	if _, ok := m.federatedClient("public-client", "https://app.example.com/callback"); !ok {
		t.Fatal("client without redirect restrictions should accept any non-empty redirect_uri")
	}
}

func TestAuthorizationCodeSecurityChecks(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{OAuthClients: map[string]AccessClient{
		"client-a": {ClientSecret: "secret-a"},
		"client-b": {ClientSecret: "secret-b"},
		"public":   {Public: true, RedirectURIs: []string{"https://app.example.com/callback"}},
	}, Cache: CacheSettings{CodeStore: CodeStoreSettings{Active: "memory"}}})
	m := &Auth{cache: cache}
	defer func() { _ = m.closeCodeStore() }()

	codeStore, err := m.codeStoreRuntime(httptest.NewRequest(http.MethodGet, "/", nil).Context())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		code         oauth2store.Code
		clientID     string
		clientSecret string
		redirectURI  string
		codeVerifier string
		wantMessage  string
	}{
		{
			name:         "missing client binding",
			code:         oauth2store.Code{Alias: "victim", RedirectURI: "https://app.example.com/callback"},
			clientID:     "client-a",
			clientSecret: "secret-a",
			redirectURI:  "https://app.example.com/callback",
			wantMessage:  "code was issued to another client",
		},
		{
			name:         "wrong client",
			code:         oauth2store.Code{Alias: "victim", ClientID: "client-a", RedirectURI: "https://app.example.com/callback"},
			clientID:     "client-b",
			clientSecret: "secret-b",
			redirectURI:  "https://app.example.com/callback",
			wantMessage:  "code was issued to another client",
		},
		{
			name:         "missing redirect binding",
			code:         oauth2store.Code{Alias: "victim", ClientID: "client-a"},
			clientID:     "client-a",
			clientSecret: "secret-a",
			redirectURI:  "https://app.example.com/callback",
			wantMessage:  "redirect_uri not match",
		},
		{
			name:         "wrong redirect",
			code:         oauth2store.Code{Alias: "victim", ClientID: "client-a", RedirectURI: "https://app.example.com/callback"},
			clientID:     "client-a",
			clientSecret: "secret-a",
			redirectURI:  "https://evil.example.com/callback",
			wantMessage:  "redirect_uri not match",
		},
		{
			name:         "public client without pkce",
			code:         oauth2store.Code{Alias: "victim", ClientID: "public", RedirectURI: "https://app.example.com/callback"},
			clientID:     "public",
			redirectURI:  "https://app.example.com/callback",
			codeVerifier: "attacker-verifier",
			wantMessage:  "public clients require PKCE",
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			codeID := "binding-test-" + string(rune('a'+i))
			codeRaw, err := oauth2store.Encode(test.code)
			if err != nil {
				t.Fatal(err)
			}
			if err := codeStore.Code.Set(t.Context(), "code_"+codeID, codeRaw); err != nil {
				t.Fatal(err)
			}

			form := url.Values{
				"grant_type":    {"authorization_code"},
				"client_id":     {test.clientID},
				"client_secret": {test.clientSecret},
				"code":          {codeID},
				"redirect_uri":  {test.redirectURI},
				"code_verifier": {test.codeVerifier},
			}
			r := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			m.APIToken(w, r)

			if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), test.wantMessage) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestFederatedAuthorizationBindsStateToBrowser(t *testing.T) {
	const redirectURI = "https://app.example.com/callback"

	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		OAuthClients: map[string]AccessClient{
			"client": {ClientSecret: "secret", RedirectURIs: []string{redirectURI}},
		},
		OAuthProviders: map[string]ProviderConfig{
			"upstream": {AuthURL: "https://idp.example.com/authorize"},
		},
		OAuth2: OAuth2Settings{BaseURL: "https://auth.example"},
		Cache:  CacheSettings{CodeStore: CodeStoreSettings{Active: "memory"}},
	})
	m := &Auth{PrefixPath: "/auth", cache: cache}
	defer func() { _ = m.closeCodeStore() }()

	r := httptest.NewRequest(
		http.MethodGet,
		"https://auth.example/auth/oauth2/auth/upstream?response_type=code&client_id=client&redirect_uri="+url.QueryEscape(redirectURI),
		nil,
	)
	r.SetPathValue("provider", "upstream")
	w := httptest.NewRecorder()
	m.APIAuth(w, r)

	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := location.Query().Get("state")
	if state == "" {
		t.Fatal("provider redirect has no state")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("state cookie = %#v", cookies)
	}

	codeStore, err := m.codeStoreRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stateRaw, ok, err := codeStore.TakeState(t.Context(), state)
	if err != nil || !ok {
		t.Fatalf("take state: ok=%v err=%v", ok, err)
	}
	stateValue, err := oauth2store.Decode[oauth2store.State](stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	callback := httptest.NewRequest(http.MethodGet, "https://auth.example/auth/oauth2/code/upstream", nil)
	callback.AddCookie(cookies[0])
	if !oauth2auth.ValidStateBinding(callback, state, stateValue.BrowserBindingHash) {
		t.Fatal("issued state is not bound to its browser cookie")
	}
	if _, ok, err := codeStore.TakeState(t.Context(), state); err != nil || ok {
		t.Fatalf("state replay: ok=%v err=%v", ok, err)
	}
}

func TestFederatedCallbackStateBrowserBindingAndReplay(t *testing.T) {
	const (
		state       = "federated-state"
		binding     = "initiating-browser"
		redirectURI = "https://app.example.com/callback"
	)

	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		OAuthClients: map[string]AccessClient{
			"client": {ClientSecret: "secret", RedirectURIs: []string{redirectURI}},
		},
		OAuthProviders: map[string]ProviderConfig{
			"upstream": {TokenURL: "://invalid-token-url"},
		},
		OAuth2: OAuth2Settings{BaseURL: "https://auth.example"},
		Cache:  CacheSettings{CodeStore: CodeStoreSettings{Active: "memory"}},
	})
	m := &Auth{PrefixPath: "/auth", cache: cache}
	defer func() { _ = m.closeCodeStore() }()

	codeStore, err := m.codeStoreRuntime(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stateRaw, err := oauth2store.Encode(oauth2store.State{
		State:              state,
		ClientID:           "client",
		RedirectURI:        redirectURI,
		BrowserBindingHash: oauth2auth.StateBindingHash(binding),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := codeStore.State.Set(t.Context(), state, stateRaw); err != nil {
		t.Fatal(err)
	}

	callback := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()

		r := httptest.NewRequest(
			http.MethodGet,
			"https://auth.example/auth/oauth2/code/upstream?code=provider-code&state="+state,
			nil,
		)
		r.SetPathValue("provider", "upstream")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		m.APICodeAuth(w, r)

		return w
	}

	wrongCookieResponse := httptest.NewRecorder()
	oauth2auth.SetStateBindingCookie(
		wrongCookieResponse,
		state,
		"other-browser",
		"/auth/oauth2/code/",
		true,
		oauth2store.DefaultStateTimeout,
	)
	w := callback(wrongCookieResponse.Result().Cookies()[0])
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "state browser binding invalid") {
		t.Fatalf("wrong browser status=%d body=%s", w.Code, w.Body.String())
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("state cookie was not cleared: %#v", cookies)
	}

	correctCookieResponse := httptest.NewRecorder()
	oauth2auth.SetStateBindingCookie(
		correctCookieResponse,
		state,
		binding,
		"/auth/oauth2/code/",
		true,
		oauth2store.DefaultStateTimeout,
	)
	w = callback(correctCookieResponse.Result().Cookies()[0])
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "state not found") {
		t.Fatalf("replay status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGenerateTOTPRecoveryCodes(t *testing.T) {
	codes, hashes, err := generateTOTPRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}

	if len(codes) != totpRecoveryCodeCount || len(hashes) != totpRecoveryCodeCount {
		t.Fatalf("got %d codes, %d hashes", len(codes), len(hashes))
	}

	seen := map[string]bool{}
	for i, code := range codes {
		if len(code) != 17 || code[8] != '-' {
			t.Errorf("code format invalid: %s", code)
		}

		if hashAPIKey(code) != hashes[i] {
			t.Errorf("hash mismatch for %s", code)
		}

		if seen[code] {
			t.Errorf("duplicate code %s", code)
		}
		seen[code] = true
	}
}
