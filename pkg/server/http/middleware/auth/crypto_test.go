package auth

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestRandomHex(t *testing.T) {
	t.Parallel()

	first, err := randomHex(32)
	if err != nil {
		t.Fatalf("randomHex() error = %v", err)
	}
	second, err := randomHex(32)
	if err != nil {
		t.Fatalf("randomHex() error = %v", err)
	}

	decoded, err := hex.DecodeString(first)
	if err != nil {
		t.Fatalf("randomHex() produced invalid hex: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("randomHex() decoded length = %d, want 32", len(decoded))
	}
	if first == second {
		t.Fatal("randomHex() returned duplicate values")
	}
}

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher("test-key")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	got, err := c.EncryptString(`{"secret":"value"}`)
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}
	if got == `{"secret":"value"}` {
		t.Fatal("expected encrypted value to differ from plaintext")
	}

	plain, err := c.DecryptString(got)
	if err != nil {
		t.Fatalf("DecryptString() error = %v", err)
	}
	if plain != `{"secret":"value"}` {
		t.Fatalf("DecryptString() = %s", plain)
	}
}

func TestCipherRekey(t *testing.T) {
	c, err := NewCipher("old-key")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	sealedOld, err := c.EncryptString("payload")
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}

	if err := c.Rekey("new-key"); err != nil {
		t.Fatalf("Rekey() error = %v", err)
	}

	// data sealed with the old key while rotating still opens
	if plain, err := c.DecryptString(sealedOld); err != nil || plain != "payload" {
		t.Fatalf("DecryptString() old value = %q, %v", plain, err)
	}

	// after a second rotation the first key is gone
	if err := c.Rekey("third-key"); err != nil {
		t.Fatal(err)
	}

	if _, err := c.DecryptString(sealedOld); err == nil {
		t.Fatal("expected decrypt failure for value sealed two keys ago")
	}

	// new key round-trips
	sealedNew, err := c.EncryptString("payload")
	if err != nil {
		t.Fatalf("EncryptString() after rekey error = %v", err)
	}

	plain, err := c.DecryptString(sealedNew)
	if err != nil {
		t.Fatalf("DecryptString() after rekey error = %v", err)
	}
	if plain != "payload" {
		t.Fatalf("DecryptString() after rekey = %s", plain)
	}
}

func TestCipherKDFVersions(t *testing.T) {
	salt := []byte("0123456789abcdef")

	legacy, err := NewCipher("short-pass")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := legacy.EncryptString("secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v1, encryptedPrefixV1) {
		t.Fatalf("without salt the value must stay enc:v1: %q", v1)
	}

	c, err := NewCipherWithSalt("short-pass", salt)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := c.EncryptString("secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v2, encryptedPrefixV2) {
		t.Fatalf("with salt the value must be enc:v2: %q", v2)
	}

	// both versions open with the salted cipher
	for _, value := range []string{v1, v2} {
		if plain, err := c.DecryptString(value); err != nil || plain != "secret" {
			t.Fatalf("decrypt %q = %q, %v", value[:7], plain, err)
		}
	}
	if c.IsCurrent(v1) || !c.IsCurrent(v2) {
		t.Fatal("IsCurrent must flag enc:v1 values for re-encryption")
	}

	// the argon2id key differs from the sha256 one
	if _, err := legacy.DecryptString(v2); err == nil {
		t.Fatal("enc:v2 must not open without the salt")
	}

	// a different salt derives a different key
	other, err := NewCipherWithSalt("short-pass", []byte("fedcba9876543210"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.DecryptString(v2); err == nil {
		t.Fatal("enc:v2 must not open with another salt")
	}

	t.Run("lazy salt loader", func(t *testing.T) {
		lazy, err := NewCipher("short-pass")
		if err != nil {
			t.Fatal(err)
		}
		lazy.SetSaltLoader(func() ([]byte, error) { return salt, nil })

		if plain, err := lazy.DecryptString(v2); err != nil || plain != "secret" {
			t.Fatalf("lazy decrypt = %q, %v", plain, err)
		}
		if sealed, _ := lazy.EncryptString("x"); !strings.HasPrefix(sealed, encryptedPrefixV2) {
			t.Fatal("after loading the salt new values must be enc:v2")
		}
	})

	t.Run("raw base64 key is used as-is", func(t *testing.T) {
		key, err := NewEncryptionKey()
		if err != nil {
			t.Fatal(err)
		}
		if IsWeakEncryptionKey(key) {
			t.Fatal("random key must not be weak")
		}

		a, _ := NewCipherWithSalt(key, salt)
		b, _ := NewCipherWithSalt(key, []byte("fedcba9876543210"))
		sealed, _ := a.EncryptString("x")
		if plain, err := b.DecryptString(sealed); err != nil || plain != "x" {
			t.Fatalf("raw key must not depend on the salt: %q, %v", plain, err)
		}
	})

	t.Run("rekey keeps salt", func(t *testing.T) {
		c, _ := NewCipherWithSalt("old-passphrase", salt)
		old, _ := c.EncryptString("x")
		if err := c.Rekey("new-passphrase"); err != nil {
			t.Fatal(err)
		}
		if plain, err := c.DecryptString(old); err != nil || plain != "x" {
			t.Fatalf("previous key must open: %q, %v", plain, err)
		}
		sealed, _ := c.EncryptString("y")
		if !strings.HasPrefix(sealed, encryptedPrefixV2) {
			t.Fatal("rekeyed cipher must keep enc:v2")
		}
	})

	if !IsWeakEncryptionKey("change-me") || IsWeakEncryptionKey("a-long-enough-passphrase") {
		t.Fatal("IsWeakEncryptionKey thresholds")
	}
}
