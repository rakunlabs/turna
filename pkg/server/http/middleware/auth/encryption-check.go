package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// encryptionCanary is the known plaintext sealed under the active encryption
// key. Decrypting it on startup proves the configured key matches the one that
// wrote the existing data.
const encryptionCanary = "turna-auth-encryption-canary-v1"

// verifyEncryptionKey fails fast when the configured encryption.key cannot
// decrypt existing data. On first start (no canary row yet) it seals the canary
// with the active key; on later starts it decrypts the stored canary and
// returns a clear error when the key does not match, instead of surfacing
// opaque AES-GCM failures deeper in the request path.
func (m *Auth) verifyEncryptionKey(ctx context.Context) error {
	var sealed string
	err := m.db.QueryRowContext(ctx,
		`SELECT canary_encrypted FROM auth_encryption_check WHERE id = true`,
	).Scan(&sealed)

	if errors.Is(err, sql.ErrNoRows) {
		return m.writeEncryptionCanary(ctx)
	}
	if err != nil {
		return fmt.Errorf("read encryption canary: %w", err)
	}

	plain, decErr := m.cipher.DecryptString(sealed)
	if decErr != nil || plain != encryptionCanary {
		return errors.New("encryption key mismatch: configured encryption.key cannot decrypt stored data; restore the previous key or rotate the encryption key from the auth UI")
	}

	return nil
}

// writeEncryptionCanary seals the canary with the active key and stores it.
// It is also used after an encryption-key rotation to refresh the canary.
func (m *Auth) writeEncryptionCanary(ctx context.Context) error {
	sealed, err := m.cipher.EncryptString(encryptionCanary)
	if err != nil {
		return fmt.Errorf("seal encryption canary: %w", err)
	}

	if _, err := m.db.ExecContext(ctx,
		`INSERT INTO auth_encryption_check (id, canary_encrypted, updated_at)
		 VALUES (true, $1, now())
		 ON CONFLICT (id) DO UPDATE SET canary_encrypted = EXCLUDED.canary_encrypted, updated_at = now()`, sealed,
	); err != nil {
		return fmt.Errorf("store encryption canary: %w", err)
	}

	return nil
}
