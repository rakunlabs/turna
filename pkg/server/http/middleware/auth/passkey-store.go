package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rakunlabs/ada/middleware/auth/strategy/passkey"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// PasskeyCredentialMeta is the listing shape for stored passkeys.
type PasskeyCredentialMeta struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	SignCount uint32 `json:"sign_count"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func passkeyCredentialKey(credentialID []byte) string {
	return passkey.Base64URLEncode(credentialID)
}

// CreatePasskeyCredential persists a credential produced by FinishRegistration.
func (s *Store) CreatePasskeyCredential(ctx context.Context, userID, name string, cred *passkey.Credential) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO auth_passkey_credentials
		(id, user_id, name, credential, sign_count)
		VALUES ($1, $2, $3, $4::jsonb, $5)`,
		passkeyCredentialKey(cred.ID), userID, name, string(raw), int64(cred.SignCount))
	if err != nil {
		return fmt.Errorf("insert passkey credential: %w", err)
	}

	return nil
}

// GetPasskeyCredential loads a credential and its owner by raw credential ID.
func (s *Store) GetPasskeyCredential(ctx context.Context, credentialID []byte) (string, *passkey.Credential, error) {
	var userID string
	var raw []byte
	var signCount int64

	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, credential, sign_count FROM auth_passkey_credentials WHERE id = $1`,
		passkeyCredentialKey(credentialID),
	).Scan(&userID, &raw, &signCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, fmt.Errorf("passkey credential not found; %w", data.ErrNotFound)
		}

		return "", nil, err
	}

	var cred passkey.Credential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return "", nil, err
	}

	cred.SignCount = uint32(signCount) //nolint:gosec // stored from uint32

	return userID, &cred, nil
}

// ListPasskeyCredentials returns credential metadata for a user.
func (s *Store) ListPasskeyCredentials(ctx context.Context, userID string) ([]PasskeyCredentialMeta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, name, sign_count,
		to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM auth_passkey_credentials WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []PasskeyCredentialMeta{}
	for rows.Next() {
		var meta PasskeyCredentialMeta
		var signCount int64
		if err := rows.Scan(&meta.ID, &meta.UserID, &meta.Name, &signCount, &meta.CreatedAt, &meta.UpdatedAt); err != nil {
			return nil, err
		}

		meta.SignCount = uint32(signCount) //nolint:gosec // stored from uint32
		list = append(list, meta)
	}

	return list, rows.Err()
}

func (s *Store) GetPasskeyCredentialMeta(ctx context.Context, id string) (*PasskeyCredentialMeta, error) {
	var meta PasskeyCredentialMeta
	var signCount int64
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, name, sign_count,
		to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM auth_passkey_credentials WHERE id = $1`, id).
		Scan(&meta.ID, &meta.UserID, &meta.Name, &signCount, &meta.CreatedAt, &meta.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("passkey credential %s not found; %w", id, data.ErrNotFound)
		}

		return nil, err
	}

	meta.SignCount = uint32(signCount) //nolint:gosec // stored from uint32

	return &meta, nil
}

// ListPasskeyCredentialIDs returns raw credential IDs for a user, used for
// allowCredentials in login and excludeCredentials in registration.
func (s *Store) ListPasskeyCredentialIDs(ctx context.Context, userID string) ([][]byte, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM auth_passkey_credentials WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := [][]byte{}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}

		raw, err := passkey.Base64URLDecode(encoded)
		if err != nil {
			continue
		}

		ids = append(ids, raw)
	}

	return ids, rows.Err()
}

// DeletePasskeyCredential removes a stored credential.
func (s *Store) DeletePasskeyCredential(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_passkey_credentials WHERE id = $1`, id)
	if err != nil {
		return err
	}

	if count, err := res.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("passkey credential %s not found; %w", id, data.ErrNotFound)
	}

	return nil
}

// UpdatePasskeySignCount persists the new sign counter after a login.
func (s *Store) UpdatePasskeySignCount(ctx context.Context, credentialID []byte, signCount uint32) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auth_passkey_credentials SET
		sign_count = $2,
		credential = jsonb_set(credential, '{SignCount}', to_jsonb($2::bigint)),
		updated_at = now()
		WHERE id = $1`,
		passkeyCredentialKey(credentialID), int64(signCount))

	return err
}
