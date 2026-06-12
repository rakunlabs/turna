package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/lib/pq"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/access"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/spf13/cast"
	"github.com/worldline-go/types"
	"golang.org/x/crypto/bcrypt"
)

// writeTx wraps a write in a transaction with version bump, event record, and notify.
func (s *Store) writeTx(ctx context.Context, topic, action, entityID string, fn func(*sql.Tx) error) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	if err := fn(tx); err != nil {
		return 0, err
	}

	version, err := nextVersion(ctx, tx)
	if err != nil {
		return 0, err
	}

	payload, _ := json.Marshal(map[string]string{"id": entityID})
	if err := insertEvent(ctx, tx, version, topic, action, entityID, payload); err != nil {
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

// ////////////////////////////////////////////////////////////////////
// user serialization

func (s *Store) marshalUser(u *data.User) (string, string, error) {
	details := u.Details

	userCopy := *u
	userCopy.Details = nil

	doc, err := json.Marshal(userCopy)
	if err != nil {
		return "", "", err
	}

	if details == nil {
		details = map[string]any{}
	}

	detailsRaw, err := json.Marshal(details)
	if err != nil {
		return "", "", err
	}

	detailsEnc, err := s.cipher.EncryptString(string(detailsRaw))
	if err != nil {
		return "", "", err
	}

	return string(doc), detailsEnc, nil
}

func (s *Store) unmarshalUser(doc []byte, detailsEnc string, disabled bool) (*data.User, error) {
	var user data.User
	if err := json.Unmarshal(doc, &user); err != nil {
		return nil, err
	}

	user.Disabled = disabled

	if detailsEnc != "" {
		plain, err := s.cipher.DecryptString(detailsEnc)
		if err != nil {
			return nil, err
		}

		details := map[string]any{}
		if err := json.Unmarshal([]byte(plain), &details); err != nil {
			return nil, err
		}

		if len(details) > 0 {
			user.Details = details
		}
	}

	return &user, nil
}

// isBcryptBase64 reports whether v is already a stored password hash
// (std-base64 of a bcrypt string), so it must not be hashed again.
func isBcryptBase64(v string) bool {
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return false
	}

	_, err = bcrypt.Cost(raw)

	return err == nil
}

func hashUserPassword(details map[string]any) {
	if details == nil {
		return
	}

	if v := cast.ToString(details["password"]); v != "" && !isBcryptBase64(v) {
		hashPassword, err := access.ToBcrypt([]byte(v))
		if err != nil {
			slog.Error("cannot hash password", slog.String("error", err.Error()))
			return
		}

		details["password"] = hashPassword
	}
}

func (s *Store) txUserByID(ctx context.Context, tx *sql.Tx, id string) (*data.User, error) {
	var doc types.RawJSON
	var detailsEnc types.Null[string]
	var disabled bool

	if err := tx.QueryRowContext(ctx,
		`SELECT doc, details_encrypted, disabled FROM auth_users WHERE id = $1 FOR UPDATE`, id,
	).Scan(&doc, &detailsEnc, &disabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user with id %s not found; %w", id, data.ErrNotFound)
		}

		return nil, err
	}

	return s.unmarshalUser(doc, detailsEnc.ValueOrZero(), disabled)
}

func (s *Store) txUpdateUser(ctx context.Context, tx *sql.Tx, user *data.User) error {
	doc, detailsEnc, err := s.marshalUser(user)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `UPDATE auth_users SET
			alias = $2,
			disabled = $3,
			service_account = $4,
			local = $5,
			doc = $6::jsonb,
			details_encrypted = $7,
			updated_at = now(),
			updated_by = $8
		WHERE id = $1`,
		user.ID, pq.Array(user.Alias), user.Disabled, user.ServiceAccount, user.Local, doc, detailsEnc, user.UpdatedBy)

	return err
}

func (s *Store) txAliasConflict(ctx context.Context, tx *sql.Tx, alias []string, excludeID string) error {
	if len(alias) == 0 {
		return nil
	}

	var foundID string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM auth_users WHERE alias && $1::text[] AND id <> $2 LIMIT 1`,
		pq.Array(alias), excludeID,
	).Scan(&foundID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	return fmt.Errorf("user with alias %v already exists; %w", alias, data.ErrConflict)
}

// ////////////////////////////////////////////////////////////////////
// users

func (s *Store) CreateUser(ctx context.Context, user data.User) (string, error) {
	user.ID = ulid.Make().String()

	hashUserPassword(user.Details)
	normalizeUser(&user)

	user.CreatedAt = time.Now().Format(time.RFC3339)
	user.UpdatedAt = user.CreatedAt
	user.UpdatedBy = data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "users", "create", user.ID, func(tx *sql.Tx) error {
		if err := s.txAliasConflict(ctx, tx, user.Alias, user.ID); err != nil {
			return err
		}

		doc, detailsEnc, err := s.marshalUser(&user)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO auth_users
			(id, alias, disabled, service_account, local, doc, details_encrypted, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)`,
			user.ID, pq.Array(user.Alias), user.Disabled, user.ServiceAccount, user.Local, doc, detailsEnc, user.UpdatedBy)

		return err
	})
	if err != nil {
		return "", err
	}

	return user.ID, nil
}

func (s *Store) PutUser(ctx context.Context, user data.User) error {
	hashUserPassword(user.Details)
	normalizeUser(&user)

	user.UpdatedAt = time.Now().Format(time.RFC3339)
	user.UpdatedBy = data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "users", "update", user.ID, func(tx *sql.Tx) error {
		found, err := s.txUserByID(ctx, tx, user.ID)
		if err != nil {
			return err
		}

		if err := s.txAliasConflict(ctx, tx, user.Alias, user.ID); err != nil {
			return err
		}

		user.CreatedAt = found.CreatedAt

		// preserve the existing password when the update omits it, so editing
		// other fields does not wipe the credential. The hash is never returned
		// to clients (GetUserAPI sanitizes it), so an absent password means
		// "keep current".
		if found.Details != nil && found.Details["password"] != nil {
			if user.Details == nil {
				user.Details = map[string]any{}
			}
			if user.Details["password"] == nil {
				user.Details["password"] = found.Details["password"]
			}
		}

		return s.txUpdateUser(ctx, tx, &user)
	})

	return err
}

func (s *Store) PatchUser(ctx context.Context, id string, patch data.UserPatch) error {
	_, err := s.writeTx(ctx, "users", "update", id, func(tx *sql.Tx) error {
		user, err := s.txUserByID(ctx, tx, id)
		if err != nil {
			return err
		}

		if patch.Alias != nil {
			if err := s.txAliasConflict(ctx, tx, *patch.Alias, id); err != nil {
				return err
			}

			user.Alias = *patch.Alias
		}

		if patch.Details != nil {
			hashUserPassword(*patch.Details)

			if user.Details != nil && user.Details["password"] != nil && (*patch.Details)["password"] == nil {
				(*patch.Details)["password"] = user.Details["password"]
			}

			user.Details = *patch.Details
		}

		if patch.PermissionIDs != nil {
			user.PermissionIDs = slicesUnique(*patch.PermissionIDs)
		}

		if patch.RoleIDs != nil {
			user.RoleIDs = slicesUnique(*patch.RoleIDs)
		}

		if patch.SyncRoleIDs != nil {
			user.SyncRoleIDs = slicesUnique(*patch.SyncRoleIDs)
		}

		if patch.IsActive != nil {
			user.Disabled = !*patch.IsActive
		}

		normalizeUser(user)
		user.UpdatedAt = time.Now().Format(time.RFC3339)
		user.UpdatedBy = data.CtxUserName(ctx)

		return s.txUpdateUser(ctx, tx, user)
	})

	return err
}

func (s *Store) PatchUserAccess(ctx context.Context, id string, userAccess data.UserAccess) error {
	if len(userAccess.RoleIDs) == 0 && len(userAccess.PermissionIDs) == 0 {
		return fmt.Errorf("at least one role or permission must be provided; %w", data.ErrInvalidRequest)
	}

	expires, err := userAccess.Expires()
	if err != nil {
		return fmt.Errorf("failed to get user access expiration: %w; %w", err, data.ErrInvalidRequest)
	}

	roleIDMap := make(map[string]struct{}, len(userAccess.RoleIDs))
	for _, roleID := range userAccess.RoleIDs {
		roleIDMap[roleID] = struct{}{}
	}
	permissionIDMap := make(map[string]struct{}, len(userAccess.PermissionIDs))
	for _, permissionID := range userAccess.PermissionIDs {
		permissionIDMap[permissionID] = struct{}{}
	}

	_, err = s.writeTx(ctx, "users", "update", id, func(tx *sql.Tx) error {
		user, err := s.txUserByID(ctx, tx, id)
		if err != nil {
			return err
		}

		if expires == nil {
			// remove access
			newTmpRoleIDs := make([]data.TmpID, 0, len(user.TmpRoleIDs))
			for _, tmpRole := range user.TmpRoleIDs {
				if _, ok := roleIDMap[tmpRole.ID]; !ok {
					newTmpRoleIDs = append(newTmpRoleIDs, tmpRole)
				}
			}
			user.TmpRoleIDs = newTmpRoleIDs

			newTmpPermissionIDs := make([]data.TmpID, 0, len(user.TmpPermissionIDs))
			for _, tmpPermission := range user.TmpPermissionIDs {
				if _, ok := permissionIDMap[tmpPermission.ID]; !ok {
					newTmpPermissionIDs = append(newTmpPermissionIDs, tmpPermission)
				}
			}
			user.TmpPermissionIDs = newTmpPermissionIDs
		} else {
			for tmpRole := range roleIDMap {
				index := slices.IndexFunc(user.TmpRoleIDs, func(existing data.TmpID) bool {
					return existing.ID == tmpRole
				})

				if index == -1 {
					user.TmpRoleIDs = append(user.TmpRoleIDs, data.TmpID{
						ID:        tmpRole,
						StartsAt:  userAccess.StartsAt,
						ExpiresAt: *expires,
					})

					continue
				}

				user.TmpRoleIDs[index].ExpiresAt = *expires
				user.TmpRoleIDs[index].StartsAt = userAccess.StartsAt
			}

			for tmpPermission := range permissionIDMap {
				index := slices.IndexFunc(user.TmpPermissionIDs, func(existing data.TmpID) bool {
					return existing.ID == tmpPermission
				})

				if index == -1 {
					user.TmpPermissionIDs = append(user.TmpPermissionIDs, data.TmpID{
						ID:        tmpPermission,
						StartsAt:  userAccess.StartsAt,
						ExpiresAt: *expires,
					})

					continue
				}

				user.TmpPermissionIDs[index].ExpiresAt = *expires
				user.TmpPermissionIDs[index].StartsAt = userAccess.StartsAt
			}
		}

		normalizeUser(user)
		user.UpdatedAt = time.Now().Format(time.RFC3339)
		user.UpdatedBy = data.CtxUserName(ctx)

		return s.txUpdateUser(ctx, tx, user)
	})

	return err
}

func (s *Store) UpdateUserSyncRoles(ctx context.Context, id string, roleIDs []string) error {
	_, err := s.writeTx(ctx, "users", "update", id, func(tx *sql.Tx) error {
		user, err := s.txUserByID(ctx, tx, id)
		if err != nil {
			return err
		}

		user.SyncRoleIDs = slicesUnique(roleIDs)
		normalizeUser(user)
		user.UpdatedAt = time.Now().Format(time.RFC3339)
		user.UpdatedBy = data.CtxUserName(ctx)

		return s.txUpdateUser(ctx, tx, user)
	})

	return err
}

// UpdateUserPassword sets only the password detail of a user; the plaintext
// password is bcrypt hashed in place. Other details stay untouched.
func (s *Store) UpdateUserPassword(ctx context.Context, id string, password string) error {
	_, err := s.writeTx(ctx, "users", "update", id, func(tx *sql.Tx) error {
		user, err := s.txUserByID(ctx, tx, id)
		if err != nil {
			return err
		}

		if user.Details == nil {
			user.Details = map[string]any{}
		}

		user.Details["password"] = password
		hashUserPassword(user.Details)

		normalizeUser(user)
		user.UpdatedAt = time.Now().Format(time.RFC3339)
		user.UpdatedBy = data.CtxUserName(ctx)

		return s.txUpdateUser(ctx, tx, user)
	})

	return err
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	_, err := s.writeTx(ctx, "users", "delete", id, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM auth_users WHERE id = $1`, id)
		if err != nil {
			return err
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("user with id %s not found; %w", id, data.ErrNotFound)
		}

		return nil
	})

	return err
}

// ////////////////////////////////////////////////////////////////////
// roles

func (s *Store) txRoleByID(ctx context.Context, tx *sql.Tx, id string) (*data.Role, error) {
	var doc types.RawJSON
	if err := tx.QueryRowContext(ctx, `SELECT config FROM auth_roles WHERE id = $1 FOR UPDATE`, id).Scan(&doc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("role with id %s not found; %w", id, data.ErrNotFound)
		}

		return nil, err
	}

	var role data.Role
	if err := json.Unmarshal(doc, &role); err != nil {
		return nil, err
	}

	return &role, nil
}

func (s *Store) txRoleNameConflict(ctx context.Context, tx *sql.Tx, name, excludeID string) error {
	var foundID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM auth_roles WHERE name = $1 AND id <> $2 LIMIT 1`, name, excludeID).Scan(&foundID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	return fmt.Errorf("role with name %s already exists; %w", name, data.ErrConflict)
}

func (s *Store) txUpdateRole(ctx context.Context, tx *sql.Tx, role *data.Role) error {
	doc, err := json.Marshal(role)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE auth_roles SET name = $2, config = $3::jsonb, updated_at = now(), updated_by = $4 WHERE id = $1`,
		role.ID, role.Name, doc, role.UpdatedBy)

	return err
}

func (s *Store) txInsertRole(ctx context.Context, tx *sql.Tx, role *data.Role) error {
	doc, err := json.Marshal(role)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO auth_roles (id, name, config, updated_by) VALUES ($1, $2, $3::jsonb, $4)`,
		role.ID, role.Name, doc, role.UpdatedBy)

	return err
}

func (s *Store) CreateRole(ctx context.Context, role data.Role) (string, error) {
	role.ID = ulid.Make().String()
	role.CreatedAt = time.Now().Format(time.RFC3339)
	role.UpdatedAt = role.CreatedAt
	role.UpdatedBy = data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "roles", "create", role.ID, func(tx *sql.Tx) error {
		if err := s.txRoleNameConflict(ctx, tx, role.Name, role.ID); err != nil {
			return err
		}

		return s.txInsertRole(ctx, tx, &role)
	})
	if err != nil {
		return "", err
	}

	return role.ID, nil
}

func (s *Store) PutRole(ctx context.Context, role data.Role) error {
	role.UpdatedAt = time.Now().Format(time.RFC3339)
	role.UpdatedBy = data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "roles", "update", role.ID, func(tx *sql.Tx) error {
		found, err := s.txRoleByID(ctx, tx, role.ID)
		if err != nil {
			return err
		}

		if err := s.txRoleNameConflict(ctx, tx, role.Name, role.ID); err != nil {
			return err
		}

		role.CreatedAt = found.CreatedAt

		return s.txUpdateRole(ctx, tx, &role)
	})

	return err
}

func (s *Store) PatchRole(ctx context.Context, id string, patch data.RolePatch) error {
	_, err := s.writeTx(ctx, "roles", "update", id, func(tx *sql.Tx) error {
		role, err := s.txRoleByID(ctx, tx, id)
		if err != nil {
			return err
		}

		if patch.Name != nil && *patch.Name != "" && *patch.Name != role.Name {
			if err := s.txRoleNameConflict(ctx, tx, *patch.Name, id); err != nil {
				return err
			}

			role.Name = *patch.Name
		}

		if patch.Description != nil {
			role.Description = *patch.Description
		}

		if patch.PermissionIDs != nil {
			role.PermissionIDs = *patch.PermissionIDs
		}

		if patch.RoleIDs != nil {
			role.RoleIDs = *patch.RoleIDs
		}

		if patch.Data != nil {
			role.Data = *patch.Data
		}

		role.UpdatedAt = time.Now().Format(time.RFC3339)
		role.UpdatedBy = data.CtxUserName(ctx)

		return s.txUpdateRole(ctx, tx, role)
	})

	return err
}

func (s *Store) DeleteRole(ctx context.Context, id string) error {
	userName := data.CtxUserName(ctx)
	now := time.Now()

	_, err := s.writeTx(ctx, "roles", "delete", id, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM auth_roles WHERE id = $1`, id)
		if err != nil {
			return err
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("role with id %s not found; %w", id, data.ErrNotFound)
		}

		// remove role from other roles
		roleRows, err := tx.QueryContext(ctx, `SELECT config FROM auth_roles WHERE config->'role_ids' ? $1 FOR UPDATE`, id)
		if err != nil {
			return err
		}

		var affectedRoles []*data.Role
		for roleRows.Next() {
			var doc types.RawJSON
			if err := roleRows.Scan(&doc); err != nil {
				roleRows.Close()
				return err
			}

			var role data.Role
			if err := json.Unmarshal(doc, &role); err != nil {
				roleRows.Close()
				return err
			}

			affectedRoles = append(affectedRoles, &role)
		}
		if err := roleRows.Err(); err != nil {
			roleRows.Close()
			return err
		}
		roleRows.Close()

		for _, role := range affectedRoles {
			role.RoleIDs = slices.DeleteFunc(role.RoleIDs, func(cmp string) bool { return cmp == id })
			role.UpdatedAt = now.Format(time.RFC3339)
			role.UpdatedBy = userName

			if err := s.txUpdateRole(ctx, tx, role); err != nil {
				return err
			}
		}

		// remove role from users
		userRows, err := tx.QueryContext(ctx, `SELECT doc, details_encrypted, disabled FROM auth_users
			WHERE doc->'role_ids' ? $1
				OR doc->'sync_role_ids' ? $1
				OR doc->'tmp_role_ids' @> jsonb_build_array(jsonb_build_object('id', $1::text))
			FOR UPDATE`, id)
		if err != nil {
			return err
		}

		var affectedUsers []*data.User
		for userRows.Next() {
			var doc types.RawJSON
			var detailsEnc types.Null[string]
			var disabled bool

			if err := userRows.Scan(&doc, &detailsEnc, &disabled); err != nil {
				userRows.Close()
				return err
			}

			user, err := s.unmarshalUser(doc, detailsEnc.ValueOrZero(), disabled)
			if err != nil {
				userRows.Close()
				return err
			}

			affectedUsers = append(affectedUsers, user)
		}
		if err := userRows.Err(); err != nil {
			userRows.Close()
			return err
		}
		userRows.Close()

		for _, user := range affectedUsers {
			user.RoleIDs = slices.DeleteFunc(user.RoleIDs, func(cmp string) bool { return cmp == id })
			user.SyncRoleIDs = slices.DeleteFunc(user.SyncRoleIDs, func(cmp string) bool { return cmp == id })
			user.TmpRoleIDs = slices.DeleteFunc(user.TmpRoleIDs, func(cmp data.TmpID) bool {
				if now.After(cmp.ExpiresAt.Time) {
					return true
				}

				return cmp.ID == id
			})

			user.UpdatedAt = now.Format(time.RFC3339)
			user.UpdatedBy = userName

			if err := s.txUpdateUser(ctx, tx, user); err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

func (s *Store) PutRoleRelation(ctx context.Context, relation map[string]data.RoleRelation) error {
	userName := data.CtxUserName(ctx)
	now := time.Now().Format(time.RFC3339)

	_, err := s.writeTx(ctx, "roles", "relation", "relation", func(tx *sql.Tx) error {
		for roleName, roleRelation := range relation {
			var roleID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM auth_roles WHERE name = $1`, roleName).Scan(&roleID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}

				return err
			}

			role, err := s.txRoleByID(ctx, tx, roleID)
			if err != nil {
				return err
			}

			if roleRelation.Roles != nil {
				roleIDs, err := s.txRoleIDsByNames(ctx, tx, *roleRelation.Roles)
				if err != nil {
					return err
				}

				role.RoleIDs = roleIDs
			}

			if roleRelation.Permissions != nil {
				permissionIDs, err := s.txPermissionIDsByNames(ctx, tx, *roleRelation.Permissions)
				if err != nil {
					return err
				}

				role.PermissionIDs = permissionIDs
			}

			role.UpdatedAt = now
			role.UpdatedBy = userName

			if err := s.txUpdateRole(ctx, tx, role); err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

func (s *Store) GetRoleRelation(ctx context.Context) (map[string]data.RoleRelation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT config FROM auth_roles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rolesByID := map[string]data.Role{}
	var roles []data.Role
	for rows.Next() {
		var doc types.RawJSON
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}

		var role data.Role
		if err := json.Unmarshal(doc, &role); err != nil {
			return nil, err
		}

		roles = append(roles, role)
		rolesByID[role.ID] = role
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	permissionRows, err := s.db.QueryContext(ctx, `SELECT config FROM auth_permissions`)
	if err != nil {
		return nil, err
	}
	defer permissionRows.Close()

	permissionsByID := map[string]data.Permission{}
	for permissionRows.Next() {
		var doc types.RawJSON
		if err := permissionRows.Scan(&doc); err != nil {
			return nil, err
		}

		var permission data.Permission
		if err := json.Unmarshal(doc, &permission); err != nil {
			return nil, err
		}

		permissionsByID[permission.ID] = permission
	}
	if err := permissionRows.Err(); err != nil {
		return nil, err
	}

	relation := make(map[string]data.RoleRelation, len(roles))
	for _, role := range roles {
		roleNames := make([]string, 0, len(role.RoleIDs))
		for _, roleID := range role.RoleIDs {
			if related, ok := rolesByID[roleID]; ok {
				roleNames = append(roleNames, related.Name)
			}
		}

		permissionNames := make([]string, 0, len(role.PermissionIDs))
		for _, permissionID := range role.PermissionIDs {
			if permission, ok := permissionsByID[permissionID]; ok {
				permissionNames = append(permissionNames, permission.Name)
			}
		}

		relation[role.Name] = data.RoleRelation{
			Roles:       &roleNames,
			Permissions: &permissionNames,
		}
	}

	return relation, nil
}

func (s *Store) txRoleIDsByNames(ctx context.Context, tx *sql.Tx, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT id FROM auth_roles WHERE name = ANY($1) ORDER BY name`, pq.Array(names))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0, len(names))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

func (s *Store) txPermissionIDsByNames(ctx context.Context, tx *sql.Tx, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT id FROM auth_permissions WHERE name = ANY($1) ORDER BY name`, pq.Array(names))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0, len(names))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// ////////////////////////////////////////////////////////////////////
// permissions

func (s *Store) txPermissionByID(ctx context.Context, tx *sql.Tx, id string) (*data.Permission, error) {
	var doc types.RawJSON
	if err := tx.QueryRowContext(ctx, `SELECT config FROM auth_permissions WHERE id = $1 FOR UPDATE`, id).Scan(&doc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("permission with id %s not found; %w", id, data.ErrNotFound)
		}

		return nil, err
	}

	var permission data.Permission
	if err := json.Unmarshal(doc, &permission); err != nil {
		return nil, err
	}

	return &permission, nil
}

func (s *Store) txPermissionNameConflict(ctx context.Context, tx *sql.Tx, name, excludeID string) error {
	var foundID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM auth_permissions WHERE name = $1 AND id <> $2 LIMIT 1`, name, excludeID).Scan(&foundID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	return fmt.Errorf("permission with name %s already exists; %w", name, data.ErrConflict)
}

func (s *Store) txUpdatePermission(ctx context.Context, tx *sql.Tx, permission *data.Permission) error {
	doc, err := json.Marshal(permission)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE auth_permissions SET name = $2, config = $3::jsonb, updated_at = now(), updated_by = $4 WHERE id = $1`,
		permission.ID, permission.Name, doc, permission.UpdatedBy)

	return err
}

func (s *Store) CreatePermission(ctx context.Context, permission data.Permission) (string, error) {
	permission.ID = ulid.Make().String()
	permission.CreatedAt = time.Now().Format(time.RFC3339)
	permission.UpdatedAt = permission.CreatedAt
	permission.UpdatedBy = data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "permissions", "create", permission.ID, func(tx *sql.Tx) error {
		if err := s.txPermissionNameConflict(ctx, tx, permission.Name, permission.ID); err != nil {
			return err
		}

		doc, err := json.Marshal(permission)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx,
			`INSERT INTO auth_permissions (id, name, config, updated_by) VALUES ($1, $2, $3::jsonb, $4)`,
			permission.ID, permission.Name, doc, permission.UpdatedBy)

		return err
	})
	if err != nil {
		return "", err
	}

	return permission.ID, nil
}

func (s *Store) CreatePermissions(ctx context.Context, permissions []data.Permission) ([]string, error) {
	ids := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		id, err := s.CreatePermission(ctx, permission)
		if err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, nil
}

func (s *Store) PutPermission(ctx context.Context, permission data.Permission) error {
	permission.UpdatedAt = time.Now().Format(time.RFC3339)
	permission.UpdatedBy = data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "permissions", "update", permission.ID, func(tx *sql.Tx) error {
		found, err := s.txPermissionByID(ctx, tx, permission.ID)
		if err != nil {
			return err
		}

		if err := s.txPermissionNameConflict(ctx, tx, permission.Name, permission.ID); err != nil {
			return err
		}

		permission.CreatedAt = found.CreatedAt

		return s.txUpdatePermission(ctx, tx, &permission)
	})

	return err
}

func (s *Store) PatchPermission(ctx context.Context, id string, patch data.PermissionPatch) error {
	_, err := s.writeTx(ctx, "permissions", "update", id, func(tx *sql.Tx) error {
		permission, err := s.txPermissionByID(ctx, tx, id)
		if err != nil {
			return err
		}

		if patch.Name != nil && *patch.Name != "" && *patch.Name != permission.Name {
			if err := s.txPermissionNameConflict(ctx, tx, *patch.Name, id); err != nil {
				return err
			}

			permission.Name = *patch.Name
		}

		if patch.Description != nil {
			permission.Description = *patch.Description
		}

		if patch.Resources != nil {
			permission.Resources = *patch.Resources
		}

		if patch.Data != nil {
			permission.Data = patch.Data
		}

		if patch.Scope != nil {
			permission.Scope = patch.Scope
		}

		permission.UpdatedAt = time.Now().Format(time.RFC3339)
		permission.UpdatedBy = data.CtxUserName(ctx)

		return s.txUpdatePermission(ctx, tx, permission)
	})

	return err
}

func (s *Store) DeletePermission(ctx context.Context, id string) error {
	userName := data.CtxUserName(ctx)
	now := time.Now()

	_, err := s.writeTx(ctx, "permissions", "delete", id, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM auth_permissions WHERE id = $1`, id)
		if err != nil {
			return err
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("permission with id %s not found; %w", id, data.ErrNotFound)
		}

		// remove permission from roles
		roleRows, err := tx.QueryContext(ctx, `SELECT config FROM auth_roles WHERE config->'permission_ids' ? $1 FOR UPDATE`, id)
		if err != nil {
			return err
		}

		var affectedRoles []*data.Role
		for roleRows.Next() {
			var doc types.RawJSON
			if err := roleRows.Scan(&doc); err != nil {
				roleRows.Close()
				return err
			}

			var role data.Role
			if err := json.Unmarshal(doc, &role); err != nil {
				roleRows.Close()
				return err
			}

			affectedRoles = append(affectedRoles, &role)
		}
		if err := roleRows.Err(); err != nil {
			roleRows.Close()
			return err
		}
		roleRows.Close()

		for _, role := range affectedRoles {
			role.PermissionIDs = slices.DeleteFunc(role.PermissionIDs, func(cmp string) bool { return cmp == id })
			role.UpdatedAt = now.Format(time.RFC3339)
			role.UpdatedBy = userName

			if err := s.txUpdateRole(ctx, tx, role); err != nil {
				return err
			}
		}

		// remove permission from users
		userRows, err := tx.QueryContext(ctx, `SELECT doc, details_encrypted, disabled FROM auth_users
			WHERE doc->'permission_ids' ? $1
				OR doc->'tmp_permission_ids' @> jsonb_build_array(jsonb_build_object('id', $1::text))
			FOR UPDATE`, id)
		if err != nil {
			return err
		}

		var affectedUsers []*data.User
		for userRows.Next() {
			var doc types.RawJSON
			var detailsEnc types.Null[string]
			var disabled bool

			if err := userRows.Scan(&doc, &detailsEnc, &disabled); err != nil {
				userRows.Close()
				return err
			}

			user, err := s.unmarshalUser(doc, detailsEnc.ValueOrZero(), disabled)
			if err != nil {
				userRows.Close()
				return err
			}

			affectedUsers = append(affectedUsers, user)
		}
		if err := userRows.Err(); err != nil {
			userRows.Close()
			return err
		}
		userRows.Close()

		for _, user := range affectedUsers {
			user.PermissionIDs = slices.DeleteFunc(user.PermissionIDs, func(cmp string) bool { return cmp == id })
			user.TmpPermissionIDs = slices.DeleteFunc(user.TmpPermissionIDs, func(cmp data.TmpID) bool {
				if now.After(cmp.ExpiresAt.Time) {
					return true
				}

				return cmp.ID == id
			})

			user.UpdatedAt = now.Format(time.RFC3339)
			user.UpdatedBy = userName

			if err := s.txUpdateUser(ctx, tx, user); err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

func (s *Store) KeepPermissions(ctx context.Context, keep map[string]struct{}) ([]data.IDName, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT config FROM auth_permissions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var permissions []data.Permission
	for rows.Next() {
		var doc types.RawJSON
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}

		var permission data.Permission
		if err := json.Unmarshal(doc, &permission); err != nil {
			return nil, err
		}

		permissions = append(permissions, permission)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	deleted := make([]data.IDName, 0)
	for _, permission := range permissions {
		if _, ok := keep[permission.Name]; ok {
			continue
		}

		if err := s.DeletePermission(ctx, permission.ID); err != nil {
			return nil, err
		}

		deleted = append(deleted, data.IDName{ID: permission.ID, Name: permission.Name})
	}

	return deleted, nil
}

// ////////////////////////////////////////////////////////////////////
// lmaps

func (s *Store) txUpsertLMap(ctx context.Context, tx *sql.Tx, lmap *data.LMap) error {
	doc, err := json.Marshal(lmap)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO auth_lmaps (name, role_ids, doc, updated_by)
		VALUES ($1, $2, $3::jsonb, $4)
		ON CONFLICT (name) DO UPDATE SET
			role_ids = EXCLUDED.role_ids,
			doc = EXCLUDED.doc,
			updated_at = now(),
			updated_by = EXCLUDED.updated_by`,
		lmap.Name, pq.Array(lmap.RoleIDs), doc, lmap.UpdatedBy)

	return err
}

func (s *Store) CreateLMap(ctx context.Context, lmap data.LMap) error {
	lmap.CreatedAt = time.Now().Format(time.RFC3339)
	lmap.UpdatedAt = lmap.CreatedAt
	lmap.UpdatedBy = data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "lmaps", "create", lmap.Name, func(tx *sql.Tx) error {
		var found string
		err := tx.QueryRowContext(ctx, `SELECT name FROM auth_lmaps WHERE name = $1`, lmap.Name).Scan(&found)
		if err == nil {
			return fmt.Errorf("lmap with name %s already exists; %w", lmap.Name, data.ErrConflict)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		return s.txUpsertLMap(ctx, tx, &lmap)
	})

	return err
}

func (s *Store) PutLMap(ctx context.Context, lmap data.LMap) error {
	lmap.UpdatedAt = time.Now().Format(time.RFC3339)
	lmap.UpdatedBy = data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "lmaps", "update", lmap.Name, func(tx *sql.Tx) error {
		var doc types.RawJSON
		err := tx.QueryRowContext(ctx, `SELECT doc FROM auth_lmaps WHERE name = $1 FOR UPDATE`, lmap.Name).Scan(&doc)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lmap with name %s not found; %w", lmap.Name, data.ErrNotFound)
		}
		if err != nil {
			return err
		}

		var old data.LMap
		if err := json.Unmarshal(doc, &old); err == nil {
			lmap.CreatedAt = old.CreatedAt
		}

		return s.txUpsertLMap(ctx, tx, &lmap)
	})

	return err
}

func (s *Store) DeleteLMap(ctx context.Context, name string) error {
	_, err := s.writeTx(ctx, "lmaps", "delete", name, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM auth_lmaps WHERE name = $1`, name)
		if err != nil {
			return err
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("lmap with name %s not found; %w", name, data.ErrNotFound)
		}

		return nil
	})

	return err
}

// EnsureLMaps creates missing roles and lmaps for the given LDAP groups.
func (s *Store) EnsureLMaps(ctx context.Context, checks []data.LMapCheckCreate) error {
	if len(checks) == 0 {
		return nil
	}

	createdAt := time.Now().Format(time.RFC3339)
	userName := data.CtxUserName(ctx)

	_, err := s.writeTx(ctx, "lmaps", "sync", "ldap", func(tx *sql.Tx) error {
		for _, check := range checks {
			var found string
			err := tx.QueryRowContext(ctx, `SELECT name FROM auth_lmaps WHERE name = $1`, check.Name).Scan(&found)
			if err == nil {
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			// find or create role with the same name
			var roleID string
			err = tx.QueryRowContext(ctx, `SELECT id FROM auth_roles WHERE name = $1`, check.Name).Scan(&roleID)
			if errors.Is(err, sql.ErrNoRows) {
				role := data.Role{
					ID:          ulid.Make().String(),
					Name:        check.Name,
					Description: check.Description,
					CreatedAt:   createdAt,
					UpdatedAt:   createdAt,
					UpdatedBy:   userName,
				}

				if err := s.txInsertRole(ctx, tx, &role); err != nil {
					return fmt.Errorf("failed to create role %s; %w", check.Name, err)
				}

				roleID = role.ID
			} else if err != nil {
				return err
			}

			lmap := data.LMap{
				Name:      check.Name,
				RoleIDs:   []string{roleID},
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				UpdatedBy: userName,
			}

			if err := s.txUpsertLMap(ctx, tx, &lmap); err != nil {
				return fmt.Errorf("failed to create lmap %s; %w", lmap.Name, err)
			}
		}

		return nil
	})

	return err
}

// ////////////////////////////////////////////////////////////////////
// load for cache

func (s *Store) LoadUsers(ctx context.Context) ([]*data.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc, details_encrypted, disabled FROM auth_users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []*data.User{}
	for rows.Next() {
		var doc types.RawJSON
		var detailsEnc types.Null[string]
		var disabled bool

		if err := rows.Scan(&doc, &detailsEnc, &disabled); err != nil {
			return nil, err
		}

		user, err := s.unmarshalUser(doc, detailsEnc.ValueOrZero(), disabled)
		if err != nil {
			return nil, err
		}

		if user.ID == "" {
			continue
		}

		users = append(users, user)
	}

	return users, rows.Err()
}

func (s *Store) LoadRoles(ctx context.Context) ([]*data.Role, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT config FROM auth_roles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := []*data.Role{}
	for rows.Next() {
		var doc types.RawJSON
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}

		var role data.Role
		if err := json.Unmarshal(doc, &role); err != nil {
			return nil, err
		}

		if role.ID == "" {
			continue
		}

		roles = append(roles, &role)
	}

	return roles, rows.Err()
}

func (s *Store) LoadPermissions(ctx context.Context) ([]*data.Permission, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT config FROM auth_permissions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permissions := []*data.Permission{}
	for rows.Next() {
		var doc types.RawJSON
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}

		var permission data.Permission
		if err := json.Unmarshal(doc, &permission); err != nil {
			return nil, err
		}

		if permission.ID == "" {
			continue
		}

		permissions = append(permissions, &permission)
	}

	return permissions, rows.Err()
}

func (s *Store) LoadLMaps(ctx context.Context) ([]*data.LMap, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, role_ids, doc FROM auth_lmaps`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lmaps := []*data.LMap{}
	for rows.Next() {
		var name string
		var roleIDs pq.StringArray
		var doc types.RawJSON

		if err := rows.Scan(&name, &roleIDs, &doc); err != nil {
			return nil, err
		}

		var lmap data.LMap
		_ = json.Unmarshal(doc, &lmap)

		lmap.Name = name
		lmap.RoleIDs = roleIDs

		lmaps = append(lmaps, &lmap)
	}

	return lmaps, rows.Err()
}

func (s *Store) LoadConfigResources(ctx context.Context, kind configKind) (map[string]types.RawJSON, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT id, config_encrypted FROM %s WHERE enabled = true`, kind.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resources := map[string]types.RawJSON{}
	for rows.Next() {
		var id, encrypted string
		if err := rows.Scan(&id, &encrypted); err != nil {
			return nil, err
		}

		plain, err := s.cipher.DecryptString(encrypted)
		if err != nil {
			return nil, err
		}

		resources[id] = types.RawJSON(plain)
	}

	return resources, rows.Err()
}

// GetSettingValue returns the decrypted setting value, or nil when missing.
func (s *Store) GetSettingValue(ctx context.Context, namespace string) (types.RawJSON, error) {
	setting, err := s.GetSetting(ctx, namespace)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return setting.Value, nil
}
