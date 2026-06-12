package auth

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B test vectors (SHA1, 8 digits truncated to 6 here).
func TestTOTPCode(t *testing.T) {
	// ASCII "12345678901234567890" in base32
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

	cases := []struct {
		unix int64
		want string // last 6 digits of the RFC 8-digit value
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}

	for _, c := range cases {
		counter := uint64(c.unix / totpPeriod)

		got, err := totpCode(secret, counter)
		if err != nil {
			t.Fatalf("totpCode(%d): %v", c.unix, err)
		}

		if got != c.want {
			t.Errorf("totpCode(%d) = %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestValidateTOTP(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	counter := uint64(now.Unix() / totpPeriod)

	code, err := totpCode(secret, counter)
	if err != nil {
		t.Fatal(err)
	}

	if !validateTOTP(secret, code, 1, now) {
		t.Error("current code should validate")
	}

	// previous period within skew
	prev, _ := totpCode(secret, counter-1)
	if !validateTOTP(secret, prev, 1, now) {
		t.Error("previous code should validate with skew 1")
	}

	if validateTOTP(secret, prev, 0, now.Add(totpPeriod*2*time.Second)) {
		t.Error("old code should not validate with skew 0")
	}

	if validateTOTP(secret, "000000", 1, now) && code != "000000" && prev != "000000" {
		t.Error("wrong code should not validate")
	}

	if validateTOTP(secret, "12345", 1, now) {
		t.Error("short code should not validate")
	}
}

func TestGenerateUserCode(t *testing.T) {
	code, err := generateUserCode()
	if err != nil {
		t.Fatal(err)
	}

	if len(code) != 9 || code[4] != '-' {
		t.Errorf("user code format invalid: %s", code)
	}

	for _, c := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(userCodeAlphabet, c) {
			t.Errorf("user code has invalid char: %s", code)
		}
	}

	if normalizeUserCode(" bcdf-ghjk ") != "BCDF-GHJK" {
		t.Error("normalizeUserCode failed")
	}

	if normalizeUserCode("bcdfghjk") != "BCDF-GHJK" {
		t.Error("normalizeUserCode without dash failed")
	}
}

func TestGenerateAPIKey(t *testing.T) {
	key, hash, err := generateAPIKey()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(key, APIKeyPrefix) {
		t.Errorf("api key missing prefix: %s", key)
	}

	if hashAPIKey(key) != hash {
		t.Error("api key hash mismatch")
	}
}

func TestRedirectAllowed(t *testing.T) {
	if !redirectAllowed("https://app.example.com/cb", nil) {
		t.Error("empty whitelist should allow")
	}

	if !redirectAllowed("https://app.example.com/cb", []string{"https://app.example.com/"}) {
		t.Error("prefix match should allow")
	}

	if redirectAllowed("https://evil.example.com/cb", []string{"https://app.example.com/"}) {
		t.Error("non-matching prefix should deny")
	}

	if redirectAllowed("", nil) {
		t.Error("empty redirect should deny")
	}
}
