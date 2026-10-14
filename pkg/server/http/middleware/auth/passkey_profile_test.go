package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasskeyEngineSelectsSiteByOrigin(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{Passkey: PasskeySettings{
		RPID:          "default.example.com",
		RPDisplayName: "Default",
		Origins:       []string{"https://default.example.com"},
		Sites: []PasskeySiteSettings{{
			Name:          "customer",
			RPID:          "customer.example",
			RPDisplayName: "Customer",
			Origins:       []string{"https://login.customer.example"},
		}},
	}})
	m := &Auth{cache: cache}

	request := httptest.NewRequest("POST", "https://login.customer.example/passkey", nil)
	_, relyingParty, err := m.passkeyEngine(request)
	if err != nil {
		t.Fatal(err)
	}
	if relyingParty.Default || relyingParty.RPID != "customer.example" || relyingParty.DisplayName != "Customer" {
		t.Fatalf("selected relying party = %+v", relyingParty)
	}

	request = httptest.NewRequest("POST", "https://default.example.com/passkey", nil)
	_, relyingParty, err = m.passkeyEngine(request)
	if err != nil {
		t.Fatal(err)
	}
	if !relyingParty.Default || relyingParty.RPID != "default.example.com" {
		t.Fatalf("default relying party = %+v", relyingParty)
	}
}

func TestValidatePasskeySites(t *testing.T) {
	valid := PasskeySettings{Sites: []PasskeySiteSettings{
		{Name: "one", RPID: "one.example", Origins: []string{"https://login.one.example"}},
		{Name: "two", RPID: "two.example", Origins: []string{"https://login.two.example"}},
	}}
	if err := validatePasskeySettings(valid); err != nil {
		t.Fatalf("valid sites rejected: %v", err)
	}

	duplicate := valid
	duplicate.Sites[1].Origins = []string{"https://login.one.example"}
	if err := validatePasskeySettings(duplicate); err == nil {
		t.Fatal("duplicate origin accepted")
	}
}

func TestPasskeyRelatedOrigins(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{Passkey: PasskeySettings{
		RPID:           "example.com",
		Origins:        []string{"https://login.example.com"},
		RelatedOrigins: []string{"https://example.co.uk"},
		Sites: []PasskeySiteSettings{{
			Name:           "shop",
			RPID:           "shop.example",
			Origins:        []string{"https://login.shop.example"},
			RelatedOrigins: []string{"https://shop-rewards.example", "https://shop.de"},
		}},
	}})
	m := &Auth{cache: cache}

	// A related origin selects its profile and is accepted in client data.
	request := httptest.NewRequest("POST", "https://shop.de/passkey", nil)
	_, relyingParty, err := m.passkeyEngine(request)
	if err != nil {
		t.Fatal(err)
	}
	if relyingParty.Default || relyingParty.RPID != "shop.example" {
		t.Fatalf("related origin selected %+v", relyingParty)
	}
	if !originListed("https://shop.de", relyingParty.Origins) || !originListed("https://login.shop.example", relyingParty.Origins) {
		t.Fatalf("related origin missing from accepted origins: %v", relyingParty.Origins)
	}

	request = httptest.NewRequest("POST", "https://example.co.uk/passkey", nil)
	_, relyingParty, err = m.passkeyEngine(request)
	if err != nil {
		t.Fatal(err)
	}
	if !relyingParty.Default || relyingParty.RPID != "example.com" || !originListed("https://example.co.uk", relyingParty.Origins) {
		t.Fatalf("default related origin = %+v", relyingParty)
	}

	for _, tc := range []struct {
		host string
		want string
	}{
		{"shop.example", `{"origins":["https://shop-rewards.example","https://shop.de"]}`},
		{"example.com:443", `{"origins":["https://example.co.uk"]}`},
		{"login.shop.example", ""},
		{"unknown.test", ""},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", "https://"+tc.host+"/.well-known/webauthn", nil)
		m.WebAuthnWellKnown(recorder, request)

		if tc.want == "" {
			if recorder.Code != 404 {
				t.Fatalf("%s: status %d, want 404", tc.host, recorder.Code)
			}
			continue
		}
		if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s: status %d content-type %q", tc.host, recorder.Code, recorder.Header().Get("Content-Type"))
		}
		if got := strings.TrimSpace(recorder.Body.String()); got != tc.want {
			t.Fatalf("%s: body %s, want %s", tc.host, got, tc.want)
		}
	}
}

func TestValidatePasskeyRelatedOrigins(t *testing.T) {
	for name, tc := range map[string]struct {
		setting PasskeySettings
		ok      bool
	}{
		"valid":        {PasskeySettings{RPID: "example.com", RelatedOrigins: []string{"https://example.co.uk", "https://example.de"}}, true},
		"needs rp_id":  {PasskeySettings{RelatedOrigins: []string{"https://example.co.uk"}}, false},
		"inside rp_id": {PasskeySettings{RPID: "example.com", RelatedOrigins: []string{"https://app.example.com"}}, false},
		"path":         {PasskeySettings{RPID: "example.com", RelatedOrigins: []string{"https://example.de/login"}}, false},
		"ip rp_id":     {PasskeySettings{RPID: "10.0.0.1", RelatedOrigins: []string{"https://example.de"}}, false},
		"too many labels": {PasskeySettings{RPID: "example.com", RelatedOrigins: []string{
			"https://a.com", "https://b.com", "https://c.com", "https://d.com", "https://e.com", "https://f.com",
		}}, false},
		"same label counts once": {PasskeySettings{RPID: "example.com", RelatedOrigins: []string{
			"https://shop.com", "https://shop.co.uk", "https://shop.de", "https://shop.fr", "https://shop.it", "https://shop.es",
		}}, true},
		"claimed by a site": {PasskeySettings{
			RPID:           "example.com",
			RelatedOrigins: []string{"https://login.one.example"},
			Sites:          []PasskeySiteSettings{{Name: "one", RPID: "one.example", Origins: []string{"https://login.one.example"}}},
		}, false},
	} {
		err := validatePasskeySettings(tc.setting)
		if tc.ok && err != nil {
			t.Fatalf("%s: rejected: %v", name, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}
