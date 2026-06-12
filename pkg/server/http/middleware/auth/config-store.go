package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/worldline-go/types"
)

type ConfigMeta struct {
	ID        string     `json:"id"`
	Enabled   bool       `json:"enabled"`
	UpdatedAt types.Time `json:"updated_at"`
	UpdatedBy string     `json:"updated_by"`
}

type ConfigResource struct {
	ID        string        `json:"id"`
	Enabled   bool          `json:"enabled"`
	Config    types.RawJSON `json:"config"`
	UpdatedAt types.Time    `json:"updated_at"`
	UpdatedBy string        `json:"updated_by"`
}

type configKind struct {
	table string
	topic string
}

var (
	oauthClientKind   = configKind{table: "auth_oauth_clients", topic: "oauth_clients"}
	oauthProviderKind = configKind{table: "auth_oauth_providers", topic: "oauth_providers"}
	ldapConfigKind    = configKind{table: "auth_ldap_configs", topic: "ldap_configs"}
	samlProviderKind  = configKind{table: "auth_saml_providers", topic: "saml_providers"}
)

func (s *Store) ListOAuthClients(ctx context.Context) ([]ConfigMeta, error) {
	return s.listConfigResources(ctx, oauthClientKind)
}

func (s *Store) GetOAuthClient(ctx context.Context, id string) (*ConfigResource, error) {
	return s.getConfigResource(ctx, oauthClientKind, id)
}

func (s *Store) PutOAuthClient(ctx context.Context, id string, config json.RawMessage, enabled bool, updatedBy string) (uint64, error) {
	return s.putConfigResource(ctx, oauthClientKind, id, config, enabled, updatedBy)
}

func (s *Store) DeleteOAuthClient(ctx context.Context, id string) (uint64, error) {
	return s.deleteConfigResource(ctx, oauthClientKind, id)
}

func (s *Store) ListOAuthProviders(ctx context.Context) ([]ConfigMeta, error) {
	return s.listConfigResources(ctx, oauthProviderKind)
}

func (s *Store) GetOAuthProvider(ctx context.Context, id string) (*ConfigResource, error) {
	return s.getConfigResource(ctx, oauthProviderKind, id)
}

func (s *Store) PutOAuthProvider(ctx context.Context, id string, config json.RawMessage, enabled bool, updatedBy string) (uint64, error) {
	return s.putConfigResource(ctx, oauthProviderKind, id, config, enabled, updatedBy)
}

func (s *Store) DeleteOAuthProvider(ctx context.Context, id string) (uint64, error) {
	return s.deleteConfigResource(ctx, oauthProviderKind, id)
}

func (s *Store) ListLDAPConfigs(ctx context.Context) ([]ConfigMeta, error) {
	return s.listConfigResources(ctx, ldapConfigKind)
}

func (s *Store) GetLDAPConfig(ctx context.Context, id string) (*ConfigResource, error) {
	return s.getConfigResource(ctx, ldapConfigKind, id)
}

func (s *Store) PutLDAPConfig(ctx context.Context, id string, config json.RawMessage, enabled bool, updatedBy string) (uint64, error) {
	return s.putConfigResource(ctx, ldapConfigKind, id, config, enabled, updatedBy)
}

func (s *Store) DeleteLDAPConfig(ctx context.Context, id string) (uint64, error) {
	return s.deleteConfigResource(ctx, ldapConfigKind, id)
}

func (s *Store) ListSAMLProviders(ctx context.Context) ([]ConfigMeta, error) {
	return s.listConfigResources(ctx, samlProviderKind)
}

func (s *Store) GetSAMLProvider(ctx context.Context, id string) (*ConfigResource, error) {
	return s.getConfigResource(ctx, samlProviderKind, id)
}

func (s *Store) PutSAMLProvider(ctx context.Context, id string, config json.RawMessage, enabled bool, updatedBy string) (uint64, error) {
	return s.putConfigResource(ctx, samlProviderKind, id, config, enabled, updatedBy)
}

func (s *Store) DeleteSAMLProvider(ctx context.Context, id string) (uint64, error) {
	return s.deleteConfigResource(ctx, samlProviderKind, id)
}

func (s *Store) listConfigResources(ctx context.Context, kind configKind) ([]ConfigMeta, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT id, enabled, updated_at, updated_by FROM %s ORDER BY id`, kind.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resources := []ConfigMeta{}
	for rows.Next() {
		var resource ConfigMeta
		if err := rows.Scan(&resource.ID, &resource.Enabled, &resource.UpdatedAt, &resource.UpdatedBy); err != nil {
			return nil, err
		}

		resources = append(resources, resource)
	}

	return resources, rows.Err()
}

func (s *Store) getConfigResource(ctx context.Context, kind configKind, id string) (*ConfigResource, error) {
	var encrypted string
	resource := ConfigResource{ID: id}

	if err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT config_encrypted, enabled, updated_at, updated_by FROM %s WHERE id = $1`, kind.table), id,
	).Scan(&encrypted, &resource.Enabled, &resource.UpdatedAt, &resource.UpdatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("config resource %s not found; %w", id, ErrNotFound)
		}

		return nil, err
	}

	plain, err := s.cipher.DecryptString(encrypted)
	if err != nil {
		return nil, err
	}

	resource.Config = types.RawJSON(plain)

	return &resource, nil
}

func (s *Store) putConfigResource(ctx context.Context, kind configKind, id string, config json.RawMessage, enabled bool, updatedBy string) (uint64, error) {
	if id == "" {
		return 0, errors.New("config id is required")
	}
	if !json.Valid(config) {
		return 0, errors.New("config must be valid json")
	}

	encrypted, err := s.cipher.EncryptString(string(config))
	if err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (id, config_encrypted, enabled, updated_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			config_encrypted = EXCLUDED.config_encrypted,
			enabled = EXCLUDED.enabled,
			updated_at = now(),
			updated_by = EXCLUDED.updated_by`, kind.table), id, encrypted, enabled, updatedBy); err != nil {
		return 0, err
	}

	version, err := nextVersion(ctx, tx)
	if err != nil {
		return 0, err
	}

	payload, _ := json.Marshal(map[string]string{"id": id})
	if err := insertEvent(ctx, tx, version, kind.topic, "upsert", id, payload); err != nil {
		return 0, err
	}

	if err := notify(ctx, tx, version); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return version, nil
}

func (s *Store) deleteConfigResource(ctx context.Context, kind configKind, id string) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, kind.table), id)
	if err != nil {
		return 0, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		return 0, fmt.Errorf("config resource %s not found; %w", id, ErrNotFound)
	}

	version, err := nextVersion(ctx, tx)
	if err != nil {
		return 0, err
	}

	payload, _ := json.Marshal(map[string]string{"id": id})
	if err := insertEvent(ctx, tx, version, kind.topic, "delete", id, payload); err != nil {
		return 0, err
	}

	if err := notify(ctx, tx, version); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return version, nil
}
