package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

// FederatedIdentity links an upstream account (provider + stable subject) to
// a local user, so a federated login resolves to the account it was first
// bound to instead of whichever user happens to own a matching alias.
type FederatedIdentity struct {
	Provider    string `json:"provider"`
	Subject     string `json:"subject"`
	UserID      string `json:"user_id"`
	Alias       string `json:"alias"`
	CreatedAt   string `json:"created_at"`
	LastLoginAt string `json:"last_login_at"`
}

const federatedIdentityColumns = `provider, subject, user_id, alias,
	to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	to_char(last_login_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')`

func scanFederatedIdentity(row interface{ Scan(...any) error }) (*FederatedIdentity, error) {
	var id FederatedIdentity
	if err := row.Scan(&id.Provider, &id.Subject, &id.UserID, &id.Alias, &id.CreatedAt, &id.LastLoginAt); err != nil {
		return nil, err
	}

	return &id, nil
}

// GetFederatedIdentity returns the link for an upstream account.
func (s *Store) GetFederatedIdentity(ctx context.Context, provider, subject string) (*FederatedIdentity, error) {
	id, err := scanFederatedIdentity(s.db.QueryRowContext(ctx,
		`SELECT `+federatedIdentityColumns+` FROM auth_federated_identities WHERE provider = $1 AND subject = $2`,
		provider, subject))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("federated identity not found; %w", data.ErrNotFound)
		}

		return nil, err
	}

	return id, nil
}

// LinkFederatedIdentity binds an upstream account to a user. It refuses when
// the user is already bound to a different account of the same provider, so
// a second upstream account cannot take over a linked user by alias.
func (s *Store) LinkFederatedIdentity(ctx context.Context, provider, subject, userID, alias string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var other string
	err = tx.QueryRowContext(ctx,
		`SELECT subject FROM auth_federated_identities WHERE provider = $1 AND user_id = $2 AND subject <> $3 LIMIT 1 FOR UPDATE`,
		provider, userID, subject).Scan(&other)
	if err == nil {
		return fmt.Errorf("user is already linked to another %s account; %w", provider, data.ErrConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	res, err := tx.ExecContext(ctx, `INSERT INTO auth_federated_identities (provider, subject, user_id, alias)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, subject) DO UPDATE SET
			alias = EXCLUDED.alias,
			last_login_at = now()
		WHERE auth_federated_identities.user_id = EXCLUDED.user_id`,
		provider, subject, userID, alias)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("%s account is linked to another user; %w", provider, data.ErrConflict)
	}

	return tx.Commit()
}

// ListFederatedIdentities lists the upstream accounts linked to a user.
func (s *Store) ListFederatedIdentities(ctx context.Context, userID string) ([]FederatedIdentity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+federatedIdentityColumns+` FROM auth_federated_identities WHERE user_id = $1 ORDER BY provider, created_at`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []FederatedIdentity{}
	for rows.Next() {
		id, err := scanFederatedIdentity(rows)
		if err != nil {
			return nil, err
		}

		list = append(list, *id)
	}

	return list, rows.Err()
}

// DeleteFederatedIdentity unlinks an upstream account from a user; the next
// login through it is resolved (and linked) again.
func (s *Store) DeleteFederatedIdentity(ctx context.Context, userID, provider, subject string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_federated_identities WHERE user_id = $1 AND provider = $2 AND subject = $3`,
		userID, provider, subject)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("federated identity not found; %w", data.ErrNotFound)
	}

	return nil
}

// ////////////////////////////////////////////////////////////////////
// handlers

// ListFederatedIdentitiesAPI lists the upstream accounts linked to a user.
func (m *Auth) ListFederatedIdentitiesAPI(w http.ResponseWriter, r *http.Request) {
	list, err := m.store.ListFederatedIdentities(r.Context(), r.PathValue("id"))
	if err != nil {
		handleIAMError(w, err, "cannot list federated identities")
		return
	}

	httputil.JSON(w, http.StatusOK, Response[[]FederatedIdentity]{
		Meta:    &Meta{TotalItemCount: uint64(len(list))},
		Payload: list,
	})
}

// DeleteFederatedIdentityAPI unlinks an upstream account from a user, e.g.
// after the person's IdP account was replaced.
func (m *Auth) DeleteFederatedIdentityAPI(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	subject := r.URL.Query().Get("subject")
	if provider == "" || subject == "" {
		httputil.HandleError(w, httputil.NewError("provider and subject are required", nil, http.StatusBadRequest))
		return
	}

	if err := m.store.DeleteFederatedIdentity(r.Context(), r.PathValue("id"), provider, subject); err != nil {
		handleIAMError(w, err, "cannot unlink federated identity")
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("federated identity unlinked"))
}
