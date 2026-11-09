package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/argon2"
)

const (
	// encryptedPrefixV1 values use the legacy key derivation: base64 16/24/32
	// byte keys as-is, anything else a single unsalted SHA-256.
	encryptedPrefixV1 = "enc:v1:"
	// encryptedPrefixV2 values use a passphrase stretched with Argon2id and
	// the per-deployment salt; base64 16/24/32 byte keys are used as-is.
	encryptedPrefixV2 = "enc:v2:"
)

// Argon2id parameters for the passphrase KDF (RFC 9106 second recommended
// option). The derivation runs once per process start or key rotation.
const (
	kdfTime    = 3
	kdfMemory  = 64 * 1024
	kdfThreads = 4
	kdfSaltLen = 16

	// minPassphraseLen is the shortest raw passphrase accepted without a
	// warning.
	minPassphraseLen = 16
)

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return hex.EncodeToString(raw), nil
}

// rawKey returns the decoded key when key is a base64 16/24/32 byte AES key.
func rawKey(key string) ([]byte, bool) {
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, false
	}

	switch len(decoded) {
	case 16, 24, 32:
		return decoded, true
	}

	return nil, false
}

// IsWeakEncryptionKey reports whether key is a raw passphrase shorter than
// minPassphraseLen.
func IsWeakEncryptionKey(key string) bool {
	if _, ok := rawKey(key); ok {
		return false
	}

	return len(key) < minPassphraseLen
}

// NewEncryptionKey returns a random base64 encoded 32 byte AES key.
func NewEncryptionKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(raw), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCM(block)
}

// legacyAEAD is the enc:v1 derivation.
func legacyAEAD(key string) (cipher.AEAD, error) {
	keyBytes, ok := rawKey(key)
	if !ok {
		keyBytes = []byte(key)
		if len(keyBytes) != 16 && len(keyBytes) != 24 && len(keyBytes) != 32 {
			derived := sha256.Sum256(keyBytes)
			keyBytes = derived[:]
		}
	}

	return newGCM(keyBytes)
}

// kdfAEAD is the enc:v2 derivation.
func kdfAEAD(key string, salt []byte) (cipher.AEAD, error) {
	if keyBytes, ok := rawKey(key); ok {
		return newGCM(keyBytes)
	}

	return newGCM(argon2.IDKey([]byte(key), salt, kdfTime, kdfMemory, kdfThreads, 32))
}

// keySet is every AEAD derived from one key.
type keySet struct {
	key string
	v1  cipher.AEAD
	// v2 is nil until a KDF salt is set.
	v2 cipher.AEAD
}

func newKeySet(key string, salt []byte) (*keySet, error) {
	if key == "" {
		return nil, errors.New("encryption key is required")
	}

	v1, err := legacyAEAD(key)
	if err != nil {
		return nil, err
	}

	ks := &keySet{key: key, v1: v1}
	if len(salt) > 0 {
		if ks.v2, err = kdfAEAD(key, salt); err != nil {
			return nil, err
		}
	}

	return ks, nil
}

func (k *keySet) open(version string, nonce, sealed []byte) ([]byte, error) {
	aead := k.v1
	if version == encryptedPrefixV2 {
		aead = k.v2
	}
	if aead == nil {
		return nil, errors.New("value is sealed with the argon2id key, but no kdf salt is loaded")
	}

	return aead.Open(nil, nonce, sealed, nil)
}

// aeadBox wraps the active keys so they can be swapped atomically during an
// encryption-key rotation without racing concurrent encrypt/decrypt calls.
type aeadBox struct {
	active *keySet
	// previous is the key before the last Rekey, used only to open values
	previous *keySet
	salt     []byte
}

// Cipher seals and opens values with AES-GCM. The active key can be swapped at
// runtime via Rekey; the *Cipher pointer stays stable so holders (Store, Auth)
// never need to be re-wired.
//
// New values are sealed as enc:v2 once a KDF salt is set (SetSalt), otherwise
// as enc:v1. Both versions open.
type Cipher struct {
	box atomic.Pointer[aeadBox]

	// saltLoader reads the stored kdf salt when an enc:v2 value arrives
	// before this process loaded it (another instance upgraded meanwhile).
	saltLoader func() ([]byte, error)
	saltM      sync.Mutex
}

// SetSaltLoader registers the lazy kdf salt source.
func (c *Cipher) SetSaltLoader(fn func() ([]byte, error)) {
	c.saltLoader = fn
}

// loadSalt runs the salt loader once while no salt is set.
func (c *Cipher) loadSalt() bool {
	if c.saltLoader == nil {
		return false
	}

	c.saltM.Lock()
	defer c.saltM.Unlock()

	if len(c.Salt()) > 0 {
		return true
	}

	salt, err := c.saltLoader()
	if err != nil || len(salt) == 0 {
		return false
	}

	return c.SetSalt(salt) == nil
}

// activeKey returns the raw configured key of the active key set.
func (c *Cipher) activeKey() string {
	return c.box.Load().active.key
}

func NewCipher(key string) (*Cipher, error) {
	return NewCipherWithSalt(key, nil)
}

func NewCipherWithSalt(key string, salt []byte) (*Cipher, error) {
	ks, err := newKeySet(key, salt)
	if err != nil {
		return nil, err
	}

	c := &Cipher{}
	c.box.Store(&aeadBox{active: ks, salt: salt})

	return c, nil
}

// SetSalt enables the enc:v2 derivation for the active and previous key.
func (c *Cipher) SetSalt(salt []byte) error {
	if len(salt) == 0 {
		return errors.New("kdf salt is required")
	}

	box := c.box.Load()
	active, err := newKeySet(box.active.key, salt)
	if err != nil {
		return err
	}

	next := &aeadBox{active: active, salt: salt}
	if box.previous != nil {
		if next.previous, err = newKeySet(box.previous.key, salt); err != nil {
			return err
		}
	}

	c.box.Store(next)

	return nil
}

// Salt returns the loaded KDF salt, nil when enc:v2 is disabled.
func (c *Cipher) Salt() []byte {
	return c.box.Load().salt
}

// WithKey returns a new Cipher for key with the same KDF salt.
func (c *Cipher) WithKey(key string) (*Cipher, error) {
	return NewCipherWithSalt(key, c.Salt())
}

// Rekey atomically swaps the active key. In-flight calls keep using the AEAD
// snapshot they already loaded; subsequent calls use the new key.
func (c *Cipher) Rekey(key string) error {
	box := c.box.Load()

	ks, err := newKeySet(key, box.salt)
	if err != nil {
		return err
	}

	// rows written with the old key while the rotation ran stay readable
	c.box.Store(&aeadBox{active: ks, previous: box.active, salt: box.salt})

	return nil
}

func (c *Cipher) EncryptString(plain string) (string, error) {
	active := c.box.Load().active

	aead, prefix := active.v1, encryptedPrefixV1
	if active.v2 != nil {
		aead, prefix = active.v2, encryptedPrefixV2
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := aead.Seal(nonce, nonce, []byte(plain), nil)

	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// sealedVersion returns the version prefix of value, or "" when value is not
// sealed by Cipher.
func sealedVersion(value string) string {
	switch {
	case strings.HasPrefix(value, encryptedPrefixV2):
		return encryptedPrefixV2
	case strings.HasPrefix(value, encryptedPrefixV1):
		return encryptedPrefixV1
	}

	return ""
}

// IsCurrent reports whether value is sealed with the derivation new values
// use; false values should be re-encrypted.
func (c *Cipher) IsCurrent(value string) bool {
	if c.box.Load().active.v2 == nil {
		return true
	}

	return sealedVersion(value) == encryptedPrefixV2
}

func (c *Cipher) DecryptString(value string) (string, error) {
	version := sealedVersion(value)
	if version == "" {
		return "", errors.New("value is not encrypted with auth cipher")
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, version))
	if err != nil {
		return "", err
	}

	box := c.box.Load()
	if version == encryptedPrefixV2 && box.active.v2 == nil && c.loadSalt() {
		box = c.box.Load()
	}

	nonceSize := box.active.v1.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("encrypted value is too short")
	}

	plain, err := box.active.open(version, raw[:nonceSize], raw[nonceSize:])
	if err != nil && box.previous != nil {
		plain, err = box.previous.open(version, raw[:nonceSize], raw[nonceSize:])
	}
	if err != nil {
		return "", err
	}

	return string(plain), nil
}
