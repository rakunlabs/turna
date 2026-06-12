package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"testing"
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
