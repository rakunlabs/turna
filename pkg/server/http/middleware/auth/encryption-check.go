package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// encryptionCanary is the known plaintext sealed under the active encryption
// key. Decrypting it on startup proves the configured key matches the one that
// wrote the existing data.
const encryptionCanary = "turna-auth-encryption-canary-v1"

func newKDFSalt() ([]byte, error) {
	salt := make([]byte, kdfSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate kdf salt: %w", err)
	}

	return salt, nil
}

// verifyEncryptionKey fails fast when the configured encryption.key cannot
// decrypt existing data. On first start (no canary row yet) it enables the
// argon2id derivation and seals the canary with the active key; on later
// starts it loads the stored kdf salt, decrypts the stored canary and returns
// a clear error when the key does not match, instead of surfacing opaque
// AES-GCM failures deeper in the request path.
func (m *Auth) verifyEncryptionKey(ctx context.Context) error {
	var sealed string
	var salt []byte
	err := m.db.QueryRowContext(ctx,
		`SELECT canary_encrypted, kdf_salt FROM auth_encryption_check WHERE id = true`,
	).Scan(&sealed, &salt)

	if errors.Is(err, sql.ErrNoRows) {
		salt, err := newKDFSalt()
		if err != nil {
			return err
		}
		if err := m.cipher.SetSalt(salt); err != nil {
			return err
		}

		return m.writeEncryptionCanary(ctx)
	}
	if err != nil {
		return fmt.Errorf("read encryption canary: %w", err)
	}

	if len(salt) > 0 {
		if err := m.cipher.SetSalt(salt); err != nil {
			return err
		}
	}

	plain, decErr := m.cipher.DecryptString(sealed)
	if decErr != nil || plain != encryptionCanary {
		return errors.New("encryption key mismatch: configured encryption.key cannot decrypt stored data; restore the previous key or rotate the encryption key from the auth UI")
	}

	if len(salt) == 0 && IsWeakEncryptionKey(m.Encryption.Key) {
		slog.Warn("auth: stored secrets use the legacy sha256 key derivation of a short passphrase; upgrade with POST /v1/encryption/upgrade once every instance runs this version, or rotate to a base64 32 byte key")
	} else if IsWeakEncryptionKey(m.Encryption.Key) {
		slog.Warn("auth: encryption.key is a short passphrase; use a base64 encoded 32 byte random key")
	}

	return nil
}

// writeEncryptionCanary seals the canary with the active key and stores it
// with the kdf salt. It is also used after an encryption-key rotation to
// refresh the canary.
func (m *Auth) writeEncryptionCanary(ctx context.Context) error {
	sealed, err := m.cipher.EncryptString(encryptionCanary)
	if err != nil {
		return fmt.Errorf("seal encryption canary: %w", err)
	}

	if _, err := m.db.ExecContext(ctx,
		`INSERT INTO auth_encryption_check (id, canary_encrypted, kdf_salt, updated_at)
		 VALUES (true, $1, $2, now())
		 ON CONFLICT (id) DO UPDATE SET canary_encrypted = EXCLUDED.canary_encrypted, kdf_salt = EXCLUDED.kdf_salt, updated_at = now()`,
		sealed, nullBytes(m.cipher.Salt()),
	); err != nil {
		return fmt.Errorf("store encryption canary: %w", err)
	}

	return nil
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}

	return b
}
