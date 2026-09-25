package auth

import (
	"net/http/httptest"
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
