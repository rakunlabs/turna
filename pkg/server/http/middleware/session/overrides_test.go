package session

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReplaceEndpointHost(t *testing.T) {
	replacements := map[string]string{
		"example.com": "myweb.com", "myweb.com": "third.com",
		"example.com:8443": "[::1]:9443",
	}
	for _, tc := range []struct{ input, want string }{
		{"https://example.com/auth/token", "https://myweb.com/auth/token"},
		{"https://example.com/a%2Fb?next=https%3A%2F%2Fexample.com&host=example.com#example.com", "https://myweb.com/a%2Fb?next=https%3A%2F%2Fexample.com&host=example.com#example.com"},
		{"http://example.com:8443/token", "http://[::1]:9443/token"},
		{"https://user:pass@example.com/token", "https://user:pass@myweb.com/token"},
		{"//example.com/token", "//myweb.com/token"},
		{"https://sub.example.com/token", "https://sub.example.com/token"},
		{"https://example.com.evil/token", "https://example.com.evil/token"},
		{"https://example.com:443/token", "https://example.com:443/token"},
		{"/example.com/token", "/example.com/token"},
		{"https://%/token", "https://%/token"},
		{"", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := ReplaceEndpointHost(tc.input, replacements); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHostReplacementsAllEndpoints(t *testing.T) {
	oauth := &Oauth2{ClientID: "example.com", ClientSecret: "example.com"}
	value := reflect.ValueOf(oauth).Elem()
	for i := 0; i < value.NumField(); i++ {
		if strings.HasSuffix(value.Type().Field(i).Name, "URL") {
			value.Field(i).SetString("https://example.com/path")
		}
	}
	providers := map[string]Provider{"company": {AuthMiddleware: "local", Oauth2: oauth}, "plain": {}}
	groups := map[string]map[string]Provider{"site": providers}
	m := &Session{ProviderSource: &ProviderSource{
		HostReplacements: map[string]string{"example.com": "myweb.com"},
		Overrides:        map[string]ProviderEndpointOverride{"company": {Oauth2: OAuth2EndpointOverride{TokenURL: "https://example.com/explicit"}}},
	}}
	m.applyDynamic(providers, groups, 1, time.Now())
	got := m.dynamic.Load().providers["company"].Oauth2
	actual := reflect.ValueOf(got).Elem()
	for i := 0; i < actual.NumField(); i++ {
		name := actual.Type().Field(i).Name
		if !strings.HasSuffix(name, "URL") {
			continue
		}
		want := "https://myweb.com/path"
		if name == "TokenURL" {
			want = "https://example.com/explicit"
		}
		if actual.Field(i).String() != want {
			t.Errorf("%s = %q, want %q", name, actual.Field(i).String(), want)
		}
		if value.Field(i).String() != "https://example.com/path" {
			t.Errorf("source %s mutated", name)
		}
	}
	if got.ClientID != oauth.ClientID || got.ClientSecret != oauth.ClientSecret {
		t.Fatal("credentials changed")
	}
	if m.dynamic.Load().providerGroups["site"]["company"].Oauth2.AuthURL != "https://myweb.com/path" {
		t.Fatal("group host not replaced")
	}
	if m.dynamic.Load().providers["plain"].Oauth2 != nil {
		t.Fatal("OAuth2 created for plain provider")
	}
}
