package auth

import (
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
