package auth

import "testing"

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

	// data sealed with the old key must no longer open under the new key
	if _, err := c.DecryptString(sealedOld); err == nil {
		t.Fatal("expected decrypt failure for value sealed with the old key")
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
