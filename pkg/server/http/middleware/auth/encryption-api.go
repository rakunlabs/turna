package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// encryptedColumn identifies a table column that stores cipher-encrypted text.
type encryptedColumn struct {
	table  string
	idCol  string
	valCol string
}

// encryptedColumns lists every column re-encrypted during a key rotation.
// auth_users.details_encrypted is nullable; the rest are NOT NULL but may still
// be skipped when empty. Short-lived flow state (auth_flow_codes) and public
// passkey material are not sealed with the cipher and are intentionally absent.
var encryptedColumns = []encryptedColumn{
	{table: "auth_oauth_clients", idCol: "id", valCol: "config_encrypted"},
	{table: "auth_oauth_providers", idCol: "id", valCol: "config_encrypted"},
	{table: "auth_ldap_configs", idCol: "id", valCol: "config_encrypted"},
	{table: "auth_saml_providers", idCol: "id", valCol: "config_encrypted"},
	{table: "auth_users", idCol: "id", valCol: "details_encrypted"},
	{table: "auth_settings", idCol: "namespace", valCol: "value_encrypted"},
	{table: "auth_totp_secrets", idCol: "user_id", valCol: "secret_encrypted"},
}

type rotateEncryptionRequest struct {
	NewKey string `json:"new_key"`
}

// RotateEncryptionAPI re-encrypts every encrypted column with a new key, swaps
// the live cipher, and refreshes the startup canary. The new key MUST be set in
// the static config (encryption.key) before the next restart, otherwise the
// startup canary check fails.
func (m *Auth) RotateEncryptionAPI(w http.ResponseWriter, r *http.Request) {
	var req rotateEncryptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.HandleError(w, httputil.NewError("invalid request body", err, http.StatusBadRequest))
		return
	}

	newKey := strings.TrimSpace(req.NewKey)
	if newKey == "" {
		httputil.HandleError(w, httputil.NewError("new_key is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.rotateEncryption(r.Context(), newKey); err != nil {
		m.audit(r, AuditEvent{Event: auditKeyRotate, Outcome: auditFailure, Detail: map[string]any{"key": "encryption", "error": err.Error()}})
		httputil.HandleError(w, httputil.NewError("encryption key rotation failed", err, http.StatusInternalServerError))
		return
	}
	m.audit(r, AuditEvent{Event: auditKeyRotate, Outcome: auditSuccess, Detail: map[string]any{"key": "encryption"}})

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"message": "encryption key rotated; set encryption.key in the static config to the new value before the next restart, otherwise startup will fail",
			"rotated": true,
		},
	})
}

// UpgradeEncryptionAPI re-seals values written with the legacy sha256 key
// derivation (enc:v1) using argon2id (enc:v2). The configured key stays the
// same. Run it after every instance runs a version that reads enc:v2.
func (m *Auth) UpgradeEncryptionAPI(w http.ResponseWriter, r *http.Request) {
	upgraded, err := m.upgradeEncryption(r.Context())
	if err != nil {
		httputil.HandleError(w, httputil.NewError("encryption upgrade failed", err, http.StatusInternalServerError))
		return
	}

	message := "stored secrets already use the argon2id key derivation"
	if upgraded {
		message = "stored secrets re-sealed with the argon2id key derivation"
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"message":  message,
			"upgraded": upgraded,
		},
	})
}

// rotateEncryption performs the re-encryption inside a single transaction, then
// hot-swaps the live cipher and reloads the cache so the running instance reads
// the freshly encrypted data.
func (m *Auth) rotateEncryption(ctx context.Context, newKey string) error {
	m.encRotateM.Lock()
	defer m.encRotateM.Unlock()

	salt, err := m.ensureKDFSalt()
	if err != nil {
		return err
	}

	newCipher, err := NewCipherWithSalt(newKey, salt)
	if err != nil {
		return fmt.Errorf("build new cipher: %w", err)
	}

	// no-op when the supplied key already decrypts the canary (same key)
	if sealed, ok := m.currentCanary(ctx); ok {
		if plain, derr := newCipher.DecryptString(sealed); derr == nil && plain == encryptionCanary {
			return errors.New("new key matches the current key; nothing to rotate")
		}
	}

	return m.reencryptAll(ctx, newCipher)
}

// upgradeEncryption re-seals every enc:v1 value with the argon2id derivation
// of the current key. The key itself does not change.
func (m *Auth) upgradeEncryption(ctx context.Context) (bool, error) {
	m.encRotateM.Lock()
	defer m.encRotateM.Unlock()

	if sealed, ok := m.currentCanary(ctx); ok && sealedVersion(sealed) == encryptedPrefixV2 {
		return false, nil
	}

	salt, err := m.ensureKDFSalt()
	if err != nil {
		return false, err
	}

	newCipher, err := NewCipherWithSalt(m.cipher.activeKey(), salt)
	if err != nil {
		return false, fmt.Errorf("build cipher: %w", err)
	}

	return true, m.reencryptAll(ctx, newCipher)
}

// ensureKDFSalt returns the loaded kdf salt or a fresh one; the salt is
// persisted with the canary by reencryptAll.
func (m *Auth) ensureKDFSalt() ([]byte, error) {
	if salt := m.cipher.Salt(); len(salt) > 0 {
		return salt, nil
	}

	return newKDFSalt()
}

// reencryptAll rewrites every encrypted column and the canary with newCipher
// in one transaction, then switches the live cipher to it.
func (m *Auth) reencryptAll(ctx context.Context, newCipher *Cipher) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, col := range encryptedColumns {
		if err := reencryptColumn(ctx, tx, m.cipher, newCipher, col); err != nil {
			return err
		}
	}

	canarySealed, err := newCipher.EncryptString(encryptionCanary)
	if err != nil {
		return fmt.Errorf("seal canary: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO auth_encryption_check (id, canary_encrypted, kdf_salt, updated_at)
		 VALUES (true, $1, $2, now())
		 ON CONFLICT (id) DO UPDATE SET canary_encrypted = EXCLUDED.canary_encrypted, kdf_salt = EXCLUDED.kdf_salt, updated_at = now()`,
		canarySealed, nullBytes(newCipher.Salt()),
	); err != nil {
		return fmt.Errorf("update canary: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// swap the live cipher so the running instance reads the new ciphertext
	if err := m.cipher.SetSalt(newCipher.Salt()); err != nil {
		return fmt.Errorf("swap live cipher salt: %w", err)
	}
	if err := m.cipher.Rekey(newCipher.activeKey()); err != nil {
		return fmt.Errorf("swap live cipher: %w", err)
	}

	// rows written with the old key while the transaction ran
	for _, col := range encryptedColumns {
		if err := reencryptStale(ctx, m.db, newCipher, m.cipher, col); err != nil {
			slog.Warn("re-encrypt rows written during rotation", "table", col.table, "error", err.Error())
		}
	}

	// reload validates end-to-end that settings decrypt under the new key
	if err := m.cache.Reload(ctx); err != nil {
		return fmt.Errorf("reload cache after rotation: %w", err)
	}

	return nil
}

// reencryptColumn rewrites every non-empty value of a column from oldC to newC.
// Rows are fully read before any UPDATE so the single transaction connection is
// not busy with an open cursor while writing.
func reencryptColumn(ctx context.Context, tx *sql.Tx, oldC, newC *Cipher, col encryptedColumn) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s", col.idCol, col.valCol, col.table))
	if err != nil {
		return fmt.Errorf("read %s: %w", col.table, err)
	}

	type record struct {
		id    string
		value string
	}

	var pending []record
	for rows.Next() {
		var id string
		var value sql.NullString
		if err := rows.Scan(&id, &value); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan %s: %w", col.table, err)
		}

		if !value.Valid || value.String == "" {
			continue
		}

		pending = append(pending, record{id: id, value: value.String})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate %s: %w", col.table, err)
	}
	_ = rows.Close()

	for _, rec := range pending {
		plain, err := oldC.DecryptString(rec.value)
		if err != nil {
			return fmt.Errorf("decrypt %s/%s: %w", col.table, rec.id, err)
		}

		sealed, err := newC.EncryptString(plain)
		if err != nil {
			return fmt.Errorf("encrypt %s/%s: %w", col.table, rec.id, err)
		}

		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2", col.table, col.valCol, col.idCol),
			sealed, rec.id,
		); err != nil {
			return fmt.Errorf("update %s/%s: %w", col.table, rec.id, err)
		}
	}

	return nil
}

// reencryptStale rewrites values of a column that do not open with newC,
// using c which also holds the previous key.
func reencryptStale(ctx context.Context, db *sql.DB, newC, c *Cipher, col encryptedColumn) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s", col.idCol, col.valCol, col.table))
	if err != nil {
		return err
	}

	stale := map[string]string{}
	for rows.Next() {
		var id string
		var value sql.NullString
		if err := rows.Scan(&id, &value); err != nil {
			_ = rows.Close()
			return err
		}

		if value.Valid && value.String != "" {
			if _, err := newC.DecryptString(value.String); err != nil || !newC.IsCurrent(value.String) {
				stale[id] = value.String
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for id, value := range stale {
		plain, err := c.DecryptString(value)
		if err != nil {
			// not from this rotation, neither key opens it
			continue
		}

		sealed, err := newC.EncryptString(plain)
		if err != nil {
			return err
		}

		// only replace the value that was read, a newer write wins
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2 AND %s = $3", col.table, col.valCol, col.idCol, col.valCol),
			sealed, id, value,
		); err != nil {
			return err
		}
	}

	return nil
}

// currentCanary returns the stored canary ciphertext when present.
func (m *Auth) currentCanary(ctx context.Context) (string, bool) {
	var sealed string
	if err := m.db.QueryRowContext(ctx,
		`SELECT canary_encrypted FROM auth_encryption_check WHERE id = true`,
	).Scan(&sealed); err != nil {
		return "", false
	}

	return sealed, true
}
