package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
)

const encryptedPrefix = "enc:v1:"

// aeadBox wraps the active AEAD so it can be swapped atomically during an
// encryption-key rotation without racing concurrent encrypt/decrypt calls.
type aeadBox struct {
	aead cipher.AEAD
}

// Cipher seals and opens values with AES-GCM. The active key can be swapped at
// runtime via Rekey; the *Cipher pointer stays stable so holders (Store, Auth)
// never need to be re-wired.
type Cipher struct {
	box atomic.Pointer[aeadBox]
}

// newAEAD derives the AES-GCM AEAD from an arbitrary key string. base64 16/24/32
// byte keys are used as-is; any other input is SHA-256 derived to 32 bytes.
func newAEAD(key string) (cipher.AEAD, error) {
	if key == "" {
		return nil, errors.New("encryption key is required")
	}

	keyBytes := []byte(key)
	if decoded, err := base64.StdEncoding.DecodeString(key); err == nil {
		switch len(decoded) {
		case 16, 24, 32:
			keyBytes = decoded
		}
	}

	if len(keyBytes) != 16 && len(keyBytes) != 24 && len(keyBytes) != 32 {
		derived := sha256.Sum256(keyBytes)
		keyBytes = derived[:]
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCM(block)
}

func NewCipher(key string) (*Cipher, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}

	c := &Cipher{}
	c.box.Store(&aeadBox{aead: aead})

	return c, nil
}

// Rekey atomically swaps the active key. In-flight calls keep using the AEAD
// snapshot they already loaded; subsequent calls use the new key.
func (c *Cipher) Rekey(key string) error {
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}

	c.box.Store(&aeadBox{aead: aead})

	return nil
}

func (c *Cipher) EncryptString(plain string) (string, error) {
	aead := c.box.Load().aead

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := aead.Seal(nonce, nonce, []byte(plain), nil)

	return encryptedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) DecryptString(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return "", errors.New("value is not encrypted with auth cipher")
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil {
		return "", err
	}

	aead := c.box.Load().aead

	nonceSize := aead.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("encrypted value is too short")
	}

	plain, err := aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", err
	}

	return string(plain), nil
}
