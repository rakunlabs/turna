package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// APIKeyPrefix marks turna auth api keys.
const APIKeyPrefix = "tak_"

const apiKeyPrincipalPrefix = "api-key:"

// APIKeyMeta is the listing shape for stored api keys; the key itself is
// only returned once at creation time.
type APIKeyMeta struct {
	ID            string         `json:"id"`
	UserID        string         `json:"user_id"`
	Name          string         `json:"name"`
	RoleIDs       []string       `json:"role_ids"`
	PermissionIDs []string       `json:"permission_ids"`
	Details       map[string]any `json:"details,omitempty"`
	Disabled      bool           `json:"disabled"`
	Revision      int64          `json:"revision"`
	ExpiresAt     string         `json:"expires_at,omitempty"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
	LastUsedAt    string         `json:"last_used_at,omitempty"`
}

type APIKeyUpdate struct {
	Name          *string
	RoleIDs       *[]string
	PermissionIDs *[]string
	Details       *map[string]any
	Disabled      *bool
}

// generateAPIKey returns the plain key and its storage hash.
func generateAPIKey() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}

	key := APIKeyPrefix + hex.EncodeToString(raw)

	return key, hashAPIKey(key), nil
}

func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))

	return hex.EncodeToString(sum[:])
}

func apiKeyPrincipalSubject(id string) string {
	if id == "" || strings.HasPrefix(id, apiKeyPrincipalPrefix) {
		return id
	}

	return apiKeyPrincipalPrefix + id
}

func apiKeyIDFromPrincipal(v string) string {
	return strings.TrimPrefix(v, apiKeyPrincipalPrefix)
}

func normalizeAPIKeyMeta(meta *APIKeyMeta) {
	meta.RoleIDs = slicesUnique(meta.RoleIDs)
	meta.PermissionIDs = slicesUnique(meta.PermissionIDs)
	if meta.Details == nil {
		meta.Details = map[string]any{}
	}
}

func marshalAPIKeyJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

func scanAPIKeyMeta(scan func(dest ...any) error) (*APIKeyMeta, error) {
	var meta APIKeyMeta
	var roleRaw, permissionRaw, detailsRaw []byte

	if err := scan(&meta.ID, &meta.UserID, &meta.Name, &meta.ExpiresAt, &meta.CreatedAt,
		&meta.UpdatedAt, &meta.LastUsedAt, &roleRaw, &permissionRaw, &detailsRaw, &meta.Disabled, &meta.Revision); err != nil {
		return nil, err
	}

	if len(roleRaw) > 0 {
		if err := json.Unmarshal(roleRaw, &meta.RoleIDs); err != nil {
			return nil, err
		}
	}
	if len(permissionRaw) > 0 {
		if err := json.Unmarshal(permissionRaw, &meta.PermissionIDs); err != nil {
			return nil, err
		}
	}
	if len(detailsRaw) > 0 {
		if err := json.Unmarshal(detailsRaw, &meta.Details); err != nil {
			return nil, err
		}
	}
	normalizeAPIKeyMeta(&meta)

	return &meta, nil
}

const apiKeyMetaSelect = `SELECT id, user_id, name,
	coalesce(to_char(expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	coalesce(to_char(last_used_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	role_ids, permission_ids, details, disabled, revision
	FROM auth_api_keys`

func scanAPIKeyRows(rows *sql.Rows) ([]APIKeyMeta, error) {
	list := []APIKeyMeta{}
	for rows.Next() {
		meta, err := scanAPIKeyMeta(rows.Scan)
		if err != nil {
			return nil, err
		}

		list = append(list, *meta)
	}

	return list, rows.Err()
}

func (s *Store) queryAPIKeys(ctx context.Context, where string, args ...any) ([]APIKeyMeta, error) {
	query := apiKeyMetaSelect
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAPIKeyRows(rows)
}

// CreateAPIKey stores a new api key principal and returns its id.
func (s *Store) CreateAPIKey(ctx context.Context, meta APIKeyMeta, keyHash string, expiresAt *time.Time) (string, error) {
	meta.ID = ulid.Make().String()
	normalizeAPIKeyMeta(&meta)

	var expires sql.NullTime
	if expiresAt != nil {
		expires = sql.NullTime{Time: *expiresAt, Valid: true}
	}

	rolesRaw, err := marshalAPIKeyJSON(meta.RoleIDs)
	if err != nil {
		return "", err
	}
	permissionsRaw, err := marshalAPIKeyJSON(meta.PermissionIDs)
	if err != nil {
		return "", err
	}
	detailsRaw, err := marshalAPIKeyJSON(meta.Details)
	if err != nil {
		return "", err
	}

	if _, err := s.db.ExecContext(ctx, `INSERT INTO auth_api_keys
		(id, user_id, name, key_hash, expires_at, role_ids, permission_ids, details, disabled)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8::jsonb, $9)`,
		meta.ID, meta.UserID, meta.Name, keyHash, expires, rolesRaw, permissionsRaw, detailsRaw, meta.Disabled); err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}

	return meta.ID, nil
}

// GetAPIKeyPrincipal resolves an api key to its own principal metadata.
// Expired keys are rejected; last_used_at is updated on success.
func (s *Store) GetAPIKeyPrincipal(ctx context.Context, key string) (*APIKeyMeta, error) {
	keyHash := hashAPIKey(key)

	var storedHash string
	row := s.db.QueryRowContext(ctx, `SELECT id, user_id, name,
		coalesce(to_char(expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
		to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		coalesce(to_char(last_used_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
		role_ids, permission_ids, details, disabled, revision, key_hash
		FROM auth_api_keys
		WHERE key_hash = $1 AND NOT disabled AND (expires_at IS NULL OR expires_at > now())`, keyHash)

	meta, err := scanAPIKeyMeta(func(dest ...any) error {
		dest = append(dest, &storedHash)
		return row.Scan(dest...)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("api key not found; %w", data.ErrNotFound)
		}

		return nil, err
	}

	// constant-time compare on top of the indexed lookup
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(keyHash)) != 1 {
		return nil, fmt.Errorf("api key not found; %w", data.ErrNotFound)
	}

	_, _ = s.db.ExecContext(ctx, `UPDATE auth_api_keys SET last_used_at = now() WHERE id = $1`, meta.ID)

	return meta, nil
}

func (s *Store) GetAPIKeyPrincipalByID(ctx context.Context, id string) (*APIKeyMeta, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, user_id, name,
		coalesce(to_char(expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
		to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		coalesce(to_char(last_used_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
		role_ids, permission_ids, details, disabled, revision
		FROM auth_api_keys
		WHERE id = $1 AND NOT disabled AND (expires_at IS NULL OR expires_at > now())`, apiKeyIDFromPrincipal(id))

	meta, err := scanAPIKeyMeta(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("api key not found; %w", data.ErrNotFound)
		}

		return nil, err
	}

	return meta, nil
}

// ListAPIKeys returns api key metadata for a user.
func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]APIKeyMeta, error) {
	return s.queryAPIKeys(ctx, "user_id = $1", userID)
}

// ListAllAPIKeys returns metadata for every api key principal.
func (s *Store) ListAllAPIKeys(ctx context.Context) ([]APIKeyMeta, error) {
	return s.queryAPIKeys(ctx, "")
}

func (s *Store) GetAPIKeyMeta(ctx context.Context, id string) (*APIKeyMeta, error) {
	row := s.db.QueryRowContext(ctx, apiKeyMetaSelect+" WHERE id = $1", id)
	meta, err := scanAPIKeyMeta(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("api key %s not found; %w", id, data.ErrNotFound)
		}

		return nil, err
	}

	return meta, nil
}

func applyAPIKeyUpdate(meta *APIKeyMeta, update APIKeyUpdate) {
	if update.Name != nil {
		meta.Name = *update.Name
	}
	if update.RoleIDs != nil {
		meta.RoleIDs = slicesUnique(*update.RoleIDs)
	}
	if update.PermissionIDs != nil {
		meta.PermissionIDs = slicesUnique(*update.PermissionIDs)
	}
	if update.Details != nil {
		meta.Details = *update.Details
	}
	if update.Disabled != nil {
		meta.Disabled = *update.Disabled
	}
	normalizeAPIKeyMeta(meta)
}

func (s *Store) updateAPIKeyMeta(ctx context.Context, meta *APIKeyMeta, ownerScoped bool) error {
	rolesRaw, err := marshalAPIKeyJSON(meta.RoleIDs)
	if err != nil {
		return err
	}
	permissionsRaw, err := marshalAPIKeyJSON(meta.PermissionIDs)
	if err != nil {
		return err
	}
	detailsRaw, err := marshalAPIKeyJSON(meta.Details)
	if err != nil {
		return err
	}

	query := `UPDATE auth_api_keys SET
		name = $2,
		role_ids = $3::jsonb,
		permission_ids = $4::jsonb,
		details = $5::jsonb,
		disabled = $6,
		revision = revision + 1,
		updated_at = now()
		WHERE id = $1`
	args := []any{meta.ID, meta.Name, rolesRaw, permissionsRaw, detailsRaw, meta.Disabled}
	if ownerScoped {
		query += " AND user_id = $7"
		args = append(args, meta.UserID)
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if count, err := res.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("api key %s not found; %w", meta.ID, data.ErrNotFound)
	}

	return nil
}

// UpdateAPIKey updates metadata and access attached to a user's api key.
func (s *Store) UpdateAPIKey(ctx context.Context, userID, id string, update APIKeyUpdate) error {
	keys, err := s.ListAPIKeys(ctx, userID)
	if err != nil {
		return err
	}

	var meta *APIKeyMeta
	for i := range keys {
		if keys[i].ID == id {
			meta = &keys[i]
			break
		}
	}
	if meta == nil {
		return fmt.Errorf("api key %s not found; %w", id, data.ErrNotFound)
	}

	applyAPIKeyUpdate(meta, update)

	return s.updateAPIKeyMeta(ctx, meta, true)
}

// UpdateAPIKeyByID updates api key metadata without owner scoping.
func (s *Store) UpdateAPIKeyByID(ctx context.Context, id string, update APIKeyUpdate) error {
	meta, err := s.GetAPIKeyMeta(ctx, id)
	if err != nil {
		return err
	}
	applyAPIKeyUpdate(meta, update)

	return s.updateAPIKeyMeta(ctx, meta, false)
}

// DeleteAPIKey removes an api key owned by the user.
func (s *Store) DeleteAPIKey(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_api_keys WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}

	if count, err := res.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("api key %s not found; %w", id, data.ErrNotFound)
	}

	return nil
}

// DeleteAPIKeyByID removes an api key without owner scoping.
func (s *Store) DeleteAPIKeyByID(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_api_keys WHERE id = $1`, id)
	if err != nil {
		return err
	}

	if count, err := res.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("api key %s not found; %w", id, data.ErrNotFound)
	}

	return nil
}
