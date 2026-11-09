package auth

import (
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

func TestUserStandardClaims(t *testing.T) {
	user := &data.UserExtended{User: &data.User{Details: map[string]any{
		"name": "Jane", "uid": "jane", "email": "jane@example.com", "given_name": "Jane", "family_name": "Doe", "password": "x",
	}}}

	scopes := func(s ...string) map[string]struct{} {
		m := map[string]struct{}{}
		for _, v := range s {
			m[v] = struct{}{}
		}

		return m
	}

	legacy := userStandardClaims(user, scopes("openid"), false)
	for _, k := range []string{"name", "preferred_username", "email", "given_name", "family_name"} {
		if _, ok := legacy[k]; !ok {
			t.Errorf("legacy mode lost %s", k)
		}
	}
	if legacy["preferred_username"] != "jane" {
		t.Errorf("preferred_username = %v, want uid", legacy["preferred_username"])
	}

	if got := userStandardClaims(user, scopes("openid"), true); len(got) != 0 {
		t.Errorf("openid only must release no profile claims: %v", got)
	}

	profile := userStandardClaims(user, scopes("openid", "profile"), true)
	if profile["name"] != "Jane" || profile["family_name"] != "Doe" || profile["email"] != nil {
		t.Errorf("profile claims = %v", profile)
	}

	email := userStandardClaims(user, scopes("openid", "email"), true)
	if email["email"] != "jane@example.com" || email["name"] != nil {
		t.Errorf("email claims = %v", email)
	}

	for _, claims := range []map[string]any{legacy, profile, email} {
		if _, ok := claims["password"]; ok {
			t.Fatal("password must never be released")
		}
	}
}
