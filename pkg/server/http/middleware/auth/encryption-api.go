package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
		httputil.HandleError(w, httputil.NewError("encryption key rotation failed", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"message": "encryption key rotated; set encryption.key in the static config to the new value before the next restart, otherwise startup will fail",
			"rotated": true,
		},
	})
}

// rotateEncryption performs the re-encryption inside a single transaction, then
// hot-swaps the live cipher and reloads the cache so the running instance reads
// the freshly encrypted data.
func (m *Auth) rotateEncryption(ctx context.Context, newKey string) error {
	m.encRotateM.Lock()
	defer m.encRotateM.Unlock()

	newCipher, err := NewCipher(newKey)
	if err != nil {
		return fmt.Errorf("build new cipher: %w", err)
	}

	// no-op when the supplied key already decrypts the canary (same key)
	if sealed, ok := m.currentCanary(ctx); ok {
		if plain, derr := newCipher.DecryptString(sealed); derr == nil && plain == encryptionCanary {
			return errors.New("new key matches the current key; nothing to rotate")
		}
	}

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
		`INSERT INTO auth_encryption_check (id, canary_encrypted, updated_at)
		 VALUES (true, $1, now())
		 ON CONFLICT (id) DO UPDATE SET canary_encrypted = EXCLUDED.canary_encrypted, updated_at = now()`, canarySealed,
	); err != nil {
		return fmt.Errorf("update canary: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// swap the live cipher so the running instance reads the new ciphertext
	if err := m.cipher.Rekey(newKey); err != nil {
		return fmt.Errorf("swap live cipher: %w", err)
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
