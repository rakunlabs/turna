package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/worldline-go/types"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db     *sql.DB
	cipher *Cipher
}

type SettingMeta struct {
	Namespace string     `json:"namespace"`
	UpdatedAt types.Time `json:"updated_at"`
	UpdatedBy string     `json:"updated_by"`
}

type Setting struct {
	Namespace string        `json:"namespace"`
	Value     types.RawJSON `json:"value"`
	UpdatedAt types.Time    `json:"updated_at"`
	UpdatedBy string        `json:"updated_by"`
}

func NewStore(db *sql.DB, cipher *Cipher) *Store {
	return &Store{db: db, cipher: cipher}
}

func (s *Store) Version(ctx context.Context) (uint64, error) {
	var version uint64
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM auth_versions WHERE id = true`).Scan(&version); err != nil {
		return 0, err
	}

	return version, nil
}

func (s *Store) ListSettings(ctx context.Context) ([]SettingMeta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT namespace, updated_at, updated_by FROM auth_settings ORDER BY namespace`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := []SettingMeta{}
	for rows.Next() {
		var setting SettingMeta
		if err := rows.Scan(&setting.Namespace, &setting.UpdatedAt, &setting.UpdatedBy); err != nil {
			return nil, err
		}

		settings = append(settings, setting)
	}

	return settings, rows.Err()
}

func (s *Store) GetSetting(ctx context.Context, namespace string) (*Setting, error) {
	var encrypted string
	setting := Setting{Namespace: namespace}

	if err := s.db.QueryRowContext(ctx,
		`SELECT value_encrypted, updated_at, updated_by FROM auth_settings WHERE namespace = $1`, namespace,
	).Scan(&encrypted, &setting.UpdatedAt, &setting.UpdatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("setting %s not found; %w", namespace, ErrNotFound)
		}

		return nil, err
	}

	plain, err := s.cipher.DecryptString(encrypted)
	if err != nil {
		return nil, err
	}

	setting.Value = types.RawJSON(plain)

	return &setting, nil
}

func (s *Store) PutSetting(ctx context.Context, namespace string, value json.RawMessage, updatedBy string) (uint64, error) {
	if !json.Valid(value) {
		return 0, errors.New("setting value must be valid json")
	}

	encrypted, err := s.cipher.EncryptString(string(value))
	if err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_settings (namespace, value_encrypted, updated_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (namespace) DO UPDATE SET
			value_encrypted = EXCLUDED.value_encrypted,
			updated_at = now(),
			updated_by = EXCLUDED.updated_by`, namespace, encrypted, updatedBy); err != nil {
		return 0, err
	}

	version, err := nextVersion(ctx, tx)
	if err != nil {
		return 0, err
	}

	payload, _ := json.Marshal(map[string]string{"namespace": namespace})
	if err := insertEvent(ctx, tx, version, "settings", "upsert", namespace, payload); err != nil {
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

func (s *Store) DeleteSetting(ctx context.Context, namespace string) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `DELETE FROM auth_settings WHERE namespace = $1`, namespace)
	if err != nil {
		return 0, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		return 0, fmt.Errorf("setting %s not found; %w", namespace, ErrNotFound)
	}

	version, err := nextVersion(ctx, tx)
	if err != nil {
		return 0, err
	}

	payload, _ := json.Marshal(map[string]string{"namespace": namespace})
	if err := insertEvent(ctx, tx, version, "settings", "delete", namespace, payload); err != nil {
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

func nextVersion(ctx context.Context, tx *sql.Tx) (uint64, error) {
	var version uint64
	if err := tx.QueryRowContext(ctx,
		`UPDATE auth_versions SET version = version + 1, updated_at = now() WHERE id = true RETURNING version`,
	).Scan(&version); err != nil {
		return 0, err
	}

	return version, nil
}

func insertEvent(ctx context.Context, tx *sql.Tx, version uint64, topic, action, entityID string, payload []byte) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO auth_events (version, topic, action, entity_id, payload) VALUES ($1, $2, $3, $4, $5::jsonb)`,
		version, topic, action, entityID, string(payload),
	)

	return err
}

func notify(ctx context.Context, tx *sql.Tx, version uint64) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_notify('auth_changed', $1)`, fmt.Sprint(version))

	return err
}
