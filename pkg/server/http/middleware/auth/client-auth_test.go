package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func TestClientSecretMatches(t *testing.T) {
	for name, test := range map[string]struct {
		expected string
		provided string
		match    bool
	}{
		"matching":       {expected: "secret", provided: "secret", match: true},
		"wrong":          {expected: "secret", provided: "wrong"},
		"missing stored": {provided: "secret"},
		"missing input":  {expected: "secret"},
		"both empty":     {},
		// RFC 6749 §2.3.1: credentials in the Basic header are
		// form-urlencoded, so both forms must authenticate.
		"form encoded":         {expected: "se+cr/et=", provided: "se%2Bcr%2Fet%3D", match: true},
		"verbatim plus":        {expected: "se+cret", provided: "se+cret", match: true},
		"encoded mismatch":     {expected: "se+cret", provided: "ot%2Bher"},
		"invalid escape":       {expected: "secret", provided: "sec%zzret"},
		"encoded space stored": {expected: "sec ret", provided: "sec+ret", match: true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := clientSecretMatches(test.expected, test.provided); got != test.match {
				t.Fatalf("clientSecretMatches() = %v, want %v", got, test.match)
			}
		})
	}
}

func TestGetAccessClientRequiresConfidentialSecret(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{OAuthClients: map[string]AccessClient{
		"confidential": {ClientSecret: "secret"},
		"secretless":   {},
		"public":       {Public: true, Dynamic: true},
	}})
	m := &Auth{cache: cache}

	for name, test := range map[string]struct {
		clientID     string
		clientSecret string
		valid        bool
	}{
		"matching confidential": {clientID: "confidential", clientSecret: "secret", valid: true},
		"wrong confidential":    {clientID: "confidential", clientSecret: "wrong"},
		"missing input":         {clientID: "confidential"},
		"secretless client":     {clientID: "secretless"},
		"public client":         {clientID: "public"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := m.GetAccessClient(test.clientID, test.clientSecret)
			if (err == nil) != test.valid {
				t.Fatalf("GetAccessClient() error = %v, valid=%v", err, test.valid)
			}
		})
	}
}

// Clients following RFC 6749 §2.3.1 (Go's oauth2 package and this project's
// own client among them) form-urlencode the Basic header values, so a client
// id holding "@" or ":" arrives percent-encoded.
func TestBasicClientCredentialsDecodesEncodedID(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{OAuthClients: map[string]AccessClient{
		"plain":           {ClientSecret: "secret"},
		"svc@example.com": {ClientSecret: "se+cret"},
		"raw%2Bid":        {ClientSecret: "secret"},
		"raw+id":          {ClientSecret: "other"},
	}})
	m := &Auth{cache: cache}

	for name, test := range map[string]struct {
		clientID     string
		clientSecret string
		wantID       string
		wantSecret   string
	}{
		"plain stays": {
			clientID: "plain", clientSecret: "secret",
			wantID: "plain", wantSecret: "secret",
		},
		"encoded id decoded with its secret": {
			clientID: "svc%40example.com", clientSecret: "se%2Bcret",
			wantID: "svc@example.com", wantSecret: "se+cret",
		},
		"metadata url decoded": {
			clientID: "https%3A%2F%2Fexample.com%2Fclient", clientSecret: "",
			wantID: "https://example.com/client", wantSecret: "",
		},
		"verbatim id wins over decoded": {
			clientID: "raw%2Bid", clientSecret: "secret",
			wantID: "raw%2Bid", wantSecret: "secret",
		},
		"unknown decoded id keeps raw": {
			clientID: "no%40body", clientSecret: "secret",
			wantID: "no%40body", wantSecret: "secret",
		},
		"invalid escape keeps raw": {
			clientID: "svc%zz", clientSecret: "secret",
			wantID: "svc%zz", wantSecret: "secret",
		},
	} {
		t.Run(name, func(t *testing.T) {
			gotID, gotSecret := m.basicClientCredentials(test.clientID, test.clientSecret)
			if gotID != test.wantID || gotSecret != test.wantSecret {
				t.Fatalf("basicClientCredentials() = %q/%q, want %q/%q",
					gotID, gotSecret, test.wantID, test.wantSecret)
			}
		})
	}
}

// The full path: an encoded id and secret in the Basic header must
// authenticate the same client as the verbatim values.
func TestClientCredentialsAuthenticatesEncodedBasicHeader(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{OAuthClients: map[string]AccessClient{
		"svc@example.com": {ClientSecret: "se+cr/et="},
		"plain":           {ClientSecret: "se+cr/et="},
	}})
	m := &Auth{cache: cache}

	for name, test := range map[string]struct {
		user string
		pass string
	}{
		"encoded id and secret":    {user: url.QueryEscape("svc@example.com"), pass: url.QueryEscape("se+cr/et=")},
		"verbatim id and secret":   {user: "svc@example.com", pass: "se+cr/et="},
		"plain id encoded secret":  {user: "plain", pass: url.QueryEscape("se+cr/et=")},
		"plain id verbatim secret": {user: "plain", pass: "se+cr/et="},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://example.com/oauth2/token", nil)
			request.SetBasicAuth(test.user, test.pass)

			clientID, clientSecret := m.clientCredentials(request, AccessTokenRequest{})
			if _, err := m.GetAccessClient(clientID, clientSecret); err != nil {
				t.Fatalf("GetAccessClient(%q) = %v", clientID, err)
			}
		})
	}
}

func TestResolveClientAllowsOnlyExplicitPublicClients(t *testing.T) {
	serviceWithoutSecret := &data.User{
		ID:             "service-empty",
		Alias:          []string{"service-empty"},
		ServiceAccount: true,
		Details:        map[string]any{},
	}
	serviceWithSecret := &data.User{
		ID:             "service-secret",
		Alias:          []string{"service-secret"},
		ServiceAccount: true,
		Details:        map[string]any{"secret": "service-value"},
	}
	disabledService := &data.User{
		ID:             "service-disabled",
		Alias:          []string{"service-disabled"},
		ServiceAccount: true,
		Disabled:       true,
		Details:        map[string]any{"secret": "disabled-value"},
	}

	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		Users: map[string]*data.User{
			serviceWithoutSecret.ID: serviceWithoutSecret,
			serviceWithSecret.ID:    serviceWithSecret,
			disabledService.ID:      disabledService,
		},
		Alias: map[string]string{
			"service-empty":    serviceWithoutSecret.ID,
			"service-secret":   serviceWithSecret.ID,
			"service-disabled": disabledService.ID,
		},
		OAuthClients: map[string]AccessClient{
			"confidential": {ClientSecret: "secret"},
			"secretless":   {},
			"public":       {Public: true, Dynamic: true},
		},
	})
	m := &Auth{cache: cache}

	for name, test := range map[string]struct {
		clientID     string
		clientSecret string
		valid        bool
	}{
		"explicit public":       {clientID: "public", valid: true},
		"public with secret":    {clientID: "public", clientSecret: "attacker"},
		"secretless client":     {clientID: "secretless"},
		"secretless service":    {clientID: "service-empty"},
		"confidential":          {clientID: "confidential", clientSecret: "secret", valid: true},
		"confidential mismatch": {clientID: "confidential", clientSecret: "wrong"},
		"service confidential":  {clientID: "service-secret", clientSecret: "service-value", valid: true},
		"disabled service":      {clientID: "service-disabled", clientSecret: "disabled-value"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := m.resolveClient(test.clientID, test.clientSecret)
			if (err == nil) != test.valid {
				t.Fatalf("resolveClient() error = %v, valid=%v", err, test.valid)
			}
		})
	}
}
