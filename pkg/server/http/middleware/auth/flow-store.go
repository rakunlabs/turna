package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// flow code kinds stored in auth_flow_codes.
const (
	flowKindDevice        = "device"         // id: device_code, payload: deviceFlow
	flowKindDeviceUser    = "device_user"    // id: user_code, payload: {"device_code": ...}
	flowKindEmail         = "email"          // id: sha256(code), payload: emailFlow
	flowKindSAMLRelay     = "saml_relay"     // id: relay state, payload: samlRelay
	flowKindSignup        = "signup"         // id: sha256(code), payload: signupFlow
	flowKindPasswordReset = "password_reset" // id: sha256(code), payload: resetFlow
)

// CreateFlowCode stores a short-lived flow payload with an absolute expiry.
func (s *Store) CreateFlowCode(ctx context.Context, kind, id string, payload any, ttl time.Duration) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO auth_flow_codes (id, kind, payload, expires_at)
		VALUES ($1, $2, $3::jsonb, now() + $4 * interval '1 second')`,
		kind+":"+id, kind, string(raw), int64(ttl.Seconds()))
	if err != nil {
		return fmt.Errorf("insert flow code: %w", err)
	}

	return nil
}

// GetFlowCode loads a flow payload; expired entries count as not found.
func (s *Store) GetFlowCode(ctx context.Context, kind, id string, payload any) error {
	var raw []byte

	err := s.db.QueryRowContext(ctx,
		`SELECT payload FROM auth_flow_codes WHERE id = $1 AND kind = $2 AND expires_at > now()`,
		kind+":"+id, kind,
	).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("flow code not found; %w", data.ErrNotFound)
		}

		return err
	}

	return json.Unmarshal(raw, payload)
}

// UpdateFlowCode replaces the payload of an existing flow entry.
func (s *Store) UpdateFlowCode(ctx context.Context, kind, id string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE auth_flow_codes SET payload = $3::jsonb WHERE id = $1 AND kind = $2 AND expires_at > now()`,
		kind+":"+id, kind, string(raw))
	if err != nil {
		return err
	}

	if count, err := res.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("flow code not found; %w", data.ErrNotFound)
	}

	return nil
}

// DeleteFlowCode removes a flow entry and opportunistically prunes expired rows.
func (s *Store) DeleteFlowCode(ctx context.Context, kind, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_flow_codes WHERE id = $1 AND kind = $2`, kind+":"+id, kind)
	if err != nil {
		return err
	}

	// opportunistic cleanup of expired entries
	_, _ = s.db.ExecContext(ctx, `DELETE FROM auth_flow_codes WHERE expires_at <= now()`)

	return nil
}
