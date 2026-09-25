package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

func TestSessionProviderEndpointOverrides(t *testing.T) {
	base := map[string]session.Provider{
		"company": {Oauth2: &session.Oauth2{ClientID: "client", ClientSecret: "secret", AuthURL: "https://shared/login", TokenURL: "https://shared/token", UserInfoURL: "https://shared/userinfo"}},
		"plain":   {},
	}
	c := NewCache(nil)
	c.snap.Store(&Snapshot{Version: 1, SessionProviders: base, SessionProviderGroups: map[string]map[string]session.Provider{"site": base}})
	m := &Auth{cache: c, SessionProvidersConfig: SessionProvidersStatic{HostReplacements: map[string]string{"shared": "auth-local"}, Overrides: map[string]session.ProviderEndpointOverride{
		"company": {Oauth2: session.OAuth2EndpointOverride{AuthURL: "https://site/login", TokenURL: "https://site/token"}},
		"missing": {Oauth2: session.OAuth2EndpointOverride{TokenURL: "https://missing/token"}},
		"plain":   {Oauth2: session.OAuth2EndpointOverride{TokenURL: "https://plain/token"}},
	}}}
	check := func(providers map[string]session.Provider) {
		t.Helper()
		p := providers["company"].Oauth2
		if p.UserInfoURL != "https://auth-local/userinfo" {
			t.Fatalf("Auth host replacement failed: %+v", p)
		}
		if len(providers) != 2 || p.AuthURL != "https://site/login" || p.TokenURL != "https://site/token" || p.ClientID != "client" || p.ClientSecret != "secret" || providers["plain"].Oauth2 != nil {
			t.Fatalf("unexpected providers: %+v, oauth: %+v", providers, p)
		}
	}
	providers, _ := m.SessionProviders()
	check(providers)
	providers, _, found := m.SessionProvidersGroup("site")
	if !found {
		t.Fatal("group missing")
	}
	check(providers)
	all, groups, _ := m.SessionProviderCatalog()
	check(all)
	check(groups["site"])
	for _, group := range []string{"", "site"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/v1/session-providers/"+group, nil)
		if group == "" {
			m.SessionProvidersAPI(w, r)
		} else {
			r.SetPathValue("group", group)
			m.SessionProvidersGroupAPI(w, r)
		}
		var resp struct {
			Payload map[string]session.Provider `json:"payload"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		check(resp.Payload)
	}

	// A real in-process Session receives Auth overrides and applies its own last.
	session.IssuerRegistry.Set("endpoint-override-test", m)
	s := &session.Session{ProviderSource: &session.ProviderSource{
		AuthMiddleware: "endpoint-override-test", Group: "site",
		HostReplacements: map[string]string{"auth-local": "session-local"},
		Overrides:        map[string]session.ProviderEndpointOverride{"company": {Oauth2: session.OAuth2EndpointOverride{TokenURL: "https://consumer/token"}}},
	}}
	p := s.Providers()["company"].Oauth2
	if p.UserInfoURL != "https://session-local/userinfo" {
		t.Fatalf("Session host replacement precedence failed: %+v", p)
	}
	if p.AuthURL != "https://site/login" || p.TokenURL != "https://consumer/token" {
		t.Fatalf("precedence failed: %+v", p)
	}
	g, _ := s.ProviderGroup("site")
	if g["company"].Oauth2.TokenURL != "https://consumer/token" {
		t.Fatal("group override missing")
	}
	if base["company"].Oauth2.AuthURL != "https://shared/login" || base["company"].Oauth2.TokenURL != "https://shared/token" {
		t.Fatal("shared snapshot mutated")
	}
	check(all)
}

func TestSessionProviderHostReplacementAppliesToCodeFlowBaseURL(t *testing.T) {
	c := NewCache(nil)
	c.snap.Store(&Snapshot{OAuth2: OAuth2Settings{BaseURL: "https://shared.example.com"}})
	m := &Auth{
		PrefixPath: "/auth",
		cache:      c,
		SessionProvidersConfig: SessionProvidersStatic{HostReplacements: map[string]string{
			"shared.example.com": "site.example.com",
		}},
	}

	code, err := m.codeRuntime()
	if err != nil {
		t.Fatal(err)
	}
	redirectURL, err := code.AuthCodeRedirectURL(httptest.NewRequest(http.MethodGet, "https://internal.example.com/start", nil), "company")
	if err != nil {
		t.Fatal(err)
	}
	if redirectURL != "https://site.example.com/auth/oauth2/code/company" {
		t.Fatalf("redirect URL = %q", redirectURL)
	}
}
