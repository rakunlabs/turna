package badger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/logi"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetRoles(req data.GetRoleRequest) (*data.Response[[]data.RoleExtended], error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var roles []data.Role
	var count uint64
	var extendRoles []data.RoleExtended

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		var err error

		badgerHoldQuery := &badgerhold.Query{}

		if req.ID != "" {
			badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID).Index("ID")
		} else {
			var badgerHoldQueryInternal *badgerhold.Query
			if req.Name != "" {
				badgerHoldQueryInternal = badgerhold.Where("Name").MatchFunc(matchAll(req.Name))
			}

			if req.Description != "" {
				if badgerHoldQueryInternal != nil {
					badgerHoldQueryInternal = badgerHoldQueryInternal.And("Description").MatchFunc(matchAll(req.Description))
				} else {
					badgerHoldQueryInternal = badgerhold.Where("Description").MatchFunc(matchAll(req.Description))
				}
			}

			permissionIDs := req.PermissionIDs
			if req.Method != "" || req.Path != "" || len(req.Permissions) > 0 {
				// get permissions ids based on path and method
				newIDs, err := b.getPermissionIDs(txn, req.Method, req.Path, req.Permissions)
				if err != nil {
					return err
				}

				if len(newIDs) == 0 {
					extendRoles = []data.RoleExtended{}

					return nil
				}

				permissionIDs = append(permissionIDs, newIDs...)
			}

			if len(permissionIDs) > 0 {
				if badgerHoldQueryInternal != nil {
					badgerHoldQueryInternal = badgerHoldQueryInternal.And("PermissionIDs").ContainsAny(toInterfaceSlice(permissionIDs)...)
				} else {
					badgerHoldQueryInternal = badgerhold.Where("PermissionIDs").ContainsAny(toInterfaceSlice(permissionIDs)...)
				}
			}

			if len(req.RoleIDs) > 0 {
				if badgerHoldQueryInternal != nil {
					badgerHoldQueryInternal = badgerHoldQueryInternal.And("RoleIDs").ContainsAny(toInterfaceSlice(req.RoleIDs)...)
				} else {
					badgerHoldQueryInternal = badgerhold.Where("RoleIDs").ContainsAny(toInterfaceSlice(req.RoleIDs)...)
				}
			}

			if badgerHoldQueryInternal != nil {
				badgerHoldQuery = badgerHoldQueryInternal
			}
		}

		count, err = b.db.TxCount(txn, data.Role{}, badgerHoldQuery)
		if err != nil {
			return err
		}

		if req.Offset > 0 {
			badgerHoldQuery = badgerHoldQuery.Skip(int(req.Offset))
		}
		if req.Limit > 0 {
			badgerHoldQuery = badgerHoldQuery.Limit(int(req.Limit))
		}

		if err := b.db.TxFind(txn, &roles, badgerHoldQuery); err != nil {
			return err
		}

		extendRoles = make([]data.RoleExtended, 0, len(roles))
		for _, role := range roles {
			extendRole, err := b.ExtendRole(txn, req.AddRoles, req.AddPermissions, req.AddTotalUsers, &role)
			if err != nil {
				return err
			}

			extendRoles = append(extendRoles, extendRole)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &data.Response[[]data.RoleExtended]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: extendRoles,
	}, nil
}

func (b *Badger) GetRole(req data.GetRoleRequest) (*data.RoleExtended, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var roleExtended data.RoleExtended

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		var err error
		var role data.Role

		if err := b.db.TxGet(txn, req.ID, &role); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("role with id %s not found; %w", req.ID, data.ErrNotFound)
			}

			return err
		}

		roleExtended, err = b.ExtendRole(txn, req.AddRoles, req.AddPermissions, req.AddTotalUsers, &role)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &roleExtended, nil
}

func (b *Badger) CreateRole(ctx context.Context, role data.Role) (string, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	role.ID = ulid.Make().String()

	role.CreatedAt = time.Now().Format(time.RFC3339)
	role.UpdatedAt = role.CreatedAt
	role.UpdatedBy = data.CtxUserName(ctx)

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		// check role with name already exists
		if err := b.db.TxFindOne(txn, &data.Role{}, badgerhold.Where("Name").Eq(role.Name).Index("Name")); err != nil {
			if !errors.Is(err, badgerhold.ErrNotFound) {
				return err
			}
		} else {
			return fmt.Errorf("role with name %s already exists; %w", role.Name, data.ErrConflict)
		}

		if err := b.db.TxInsert(txn, role.ID, role); err != nil {
			if errors.Is(err, badgerhold.ErrKeyExists) {
				return fmt.Errorf("roleID %s already exists; %w", role.ID, data.ErrConflict)
			}

			return err
		}

		return nil
	}); err != nil {
		return "", err
	}

	logi.Ctx(ctx).Info("created role", slog.String("name", role.Name), slog.String("id", role.ID), slog.String("by", role.UpdatedBy))

	return role.ID, nil
}

func (b *Badger) PatchRole(ctx context.Context, id string, rolePatch data.RolePatch) error {
	return b.editRole(ctx, id, func(txn *badger.Txn, foundRole *data.Role) error {
		if rolePatch.Name != nil && *rolePatch.Name != "" && *rolePatch.Name != foundRole.Name {
			// check role with name already exists
			if err := b.db.TxFindOne(txn, &data.Role{}, badgerhold.Where("Name").Eq(*rolePatch.Name).Index("Name")); err != nil {
				if !errors.Is(err, badgerhold.ErrNotFound) {
					return err
				}
			} else {
				return fmt.Errorf("role with name %s already exists; %w", *rolePatch.Name, data.ErrConflict)
			}

			foundRole.Name = *rolePatch.Name
		}

		if rolePatch.Description != nil {
			foundRole.Description = *rolePatch.Description
		}

		if rolePatch.PermissionIDs != nil {
			foundRole.PermissionIDs = *rolePatch.PermissionIDs
		}

		if rolePatch.RoleIDs != nil {
			foundRole.RoleIDs = *rolePatch.RoleIDs
		}

		if rolePatch.Data != nil {
			foundRole.Data = *rolePatch.Data
		}

		return nil
	})
}

func (b *Badger) PutRoleRelation(ctx context.Context, relation map[string]data.RoleRelation) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		for role, roleRelation := range relation {
			// find that role's id if exists patch with rolerelation
			var foundRole data.Role
			if err := b.db.TxFindOne(txn, &foundRole, badgerhold.Where("Name").Eq(role).Index("Name")); err != nil {
				if !errors.Is(err, badgerhold.ErrNotFound) {
					return err
				}

				continue
			}

			// find id of roles and permissions
			if roleRelation.Roles != nil {
				roleIDs := make([]string, 0, len(*roleRelation.Roles))
				if len(*roleRelation.Roles) > 0 {
					if err := b.db.TxForEach(txn, badgerhold.Where("Name").In(toInterfaceSlice(*roleRelation.Roles)...), func(role *data.Role) error {
						roleIDs = append(roleIDs, role.ID)
						return nil
					}); err != nil {
						return fmt.Errorf("failed to get role IDs; %w", err)
					}
				}

				foundRole.RoleIDs = roleIDs
			}

			if roleRelation.Permissions != nil {
				permissionIDs := make([]string, 0, len(*roleRelation.Permissions))
				if len(*roleRelation.Permissions) > 0 {
					if err := b.db.TxForEach(txn, badgerhold.Where("Name").In(toInterfaceSlice(*roleRelation.Permissions)...), func(permission *data.Permission) error {
						permissionIDs = append(permissionIDs, permission.ID)
						return nil
					}); err != nil {
						return fmt.Errorf("failed to get permission IDs; %w", err)
					}
				}

				foundRole.PermissionIDs = permissionIDs
			}

			userName := data.CtxUserName(ctx)

			foundRole.UpdatedAt = time.Now().Format(time.RFC3339)
			foundRole.UpdatedBy = userName

			logi.Ctx(ctx).Info("role replaced", slog.String("name", foundRole.Name), slog.String("id", foundRole.ID), slog.String("by", foundRole.UpdatedAt))

			if err := b.db.TxUpdate(txn, foundRole.ID, foundRole); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (b *Badger) GetRoleRelation() (map[string]data.RoleRelation, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	roleRelation := make(map[string]data.RoleRelation)

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		return b.db.TxForEach(txn, badgerhold.Where("ID").Ne(""), func(role *data.Role) error {
			// get roles names
			roleNames := make([]string, 0, len(role.RoleIDs))
			if len(role.RoleIDs) > 0 {
				if err := b.db.TxForEach(txn, badgerhold.Where("ID").In(toInterfaceSlice(role.RoleIDs)...), func(role *data.Role) error {
					roleNames = append(roleNames, role.Name)
					return nil
				}); err != nil {
					return fmt.Errorf("failed to get role names; %w", err)
				}
			}

			// get permission names
			permissionNames := make([]string, 0, len(role.PermissionIDs))
			if len(role.PermissionIDs) > 0 {
				if err := b.db.TxForEach(txn, badgerhold.Where("ID").In(toInterfaceSlice(role.PermissionIDs)...), func(permission *data.Permission) error {
					permissionNames = append(permissionNames, permission.Name)
					return nil
				}); err != nil {
					return fmt.Errorf("failed to get permission names; %w", err)
				}
			}

			roleRelation[role.Name] = data.RoleRelation{
				Roles:       &roleNames,
				Permissions: &permissionNames,
			}

			return nil
		})
	}); err != nil {
		return nil, err
	}

	return roleRelation, nil
}

func (b *Badger) PutRole(ctx context.Context, role data.Role) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	role.UpdatedAt = time.Now().Format(time.RFC3339)
	role.UpdatedBy = data.CtxUserName(ctx)

	// found role with id and replace
	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		var foundRole data.Role
		if err := b.db.TxFindOne(txn, &foundRole, badgerhold.Where("ID").Eq(role.ID).Index("ID")); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("role with id %s not found; %w", role.ID, data.ErrNotFound)
			}

			return err
		}

		role.CreatedAt = foundRole.CreatedAt

		if err := b.db.TxUpdate(txn, role.ID, role); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	logi.Ctx(ctx).Info("role replaced", slog.String("id", role.ID), slog.String("name", role.Name), slog.String("by", role.UpdatedBy))

	return nil
}

func (b *Badger) DeleteRole(ctx context.Context, id string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		if err := b.db.TxDelete(txn, id, data.Role{}); err != nil {
			return err
		}

		// Delete the role from all roles
		if err := b.db.TxForEach(txn, badgerhold.Where("RoleIDs").Contains(id), func(role *data.Role) error {
			role.RoleIDs = slices.DeleteFunc(role.RoleIDs, func(cmp string) bool {
				return cmp == id
			})

			if err := b.db.TxUpdate(txn, role.ID, role); err != nil {
				return err
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to delete role from roles; %w", err)
		}

		// Delete the role from all users
		if err := b.db.TxForEach(txn, badgerhold.Where("MixRoleIDs").Contains(id), func(user *data.User) error {
			user.RoleIDs = slices.DeleteFunc(user.RoleIDs, func(cmp string) bool {
				return cmp == id
			})

			user.SyncRoleIDs = slices.DeleteFunc(user.SyncRoleIDs, func(cmp string) bool {
				return cmp == id
			})

			user.MixRoleIDs = slicesUnique(user.RoleIDs, user.SyncRoleIDs)

			if err := b.db.TxUpdate(txn, user.ID, user); err != nil {
				return err
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to delete role from users; %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	logi.Ctx(ctx).Info("role deleted", slog.String("id", id), slog.String("by", data.CtxUserName(ctx)))

	return nil
}

func (b *Badger) getVirtualRoleIDs(txn *badger.Txn, roleIDs []string) ([]string, error) {
	mapRoleIDs := make(map[string]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		mapRoleIDs[roleID] = struct{}{}
	}

	for {
		query := badgerhold.Where("ID").In(toInterfaceSlice(roleIDs)...)
		roleIDs = nil
		if err := b.db.TxForEach(txn, query, func(role *data.Role) error {
			for _, roleID := range role.RoleIDs {
				if _, ok := mapRoleIDs[roleID]; !ok {
					mapRoleIDs[roleID] = struct{}{}
					roleIDs = append(roleIDs, roleID)
				}
			}

			return nil
		}); err != nil {
			return nil, fmt.Errorf("failed to get virtual role IDs; %w", err)
		}

		if len(roleIDs) == 0 {
			break
		}
	}

	roleIDs = make([]string, 0, len(mapRoleIDs))
	for roleID := range mapRoleIDs {
		roleIDs = append(roleIDs, roleID)
	}

	return roleIDs, nil
}

func (b *Badger) getRoleIDs(txn *badger.Txn, method, path string, names []string) ([]string, error) {
	var roleIDs []string

	permissionIDs, err := b.getPermissionIDs(txn, method, path, names)
	if err != nil {
		return nil, err
	}

	query := badgerhold.Where("PermissionIDs").ContainsAny(toInterfaceSlice(permissionIDs)...)

	if err := b.db.TxForEach(txn, query, func(role *data.Role) error {
		roleIDs = append(roleIDs, role.ID)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to get role IDs; %w", err)
	}

	return roleIDs, nil
}

func (b *Badger) editRole(ctx context.Context, id string, fn func(*badger.Txn, *data.Role) error) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().Update(func(txn *badger.Txn) error {
		var foundRole data.Role
		if err := b.db.TxFindOne(txn, &foundRole, badgerhold.Where("ID").Eq(id).Index("ID")); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("role with id %s not found; %w", id, data.ErrNotFound)
			}

			return err
		}

		if err := fn(txn, &foundRole); err != nil {
			return err
		}

		foundRole.UpdatedAt = time.Now().Format(time.RFC3339)
		foundRole.UpdatedBy = data.CtxUserName(ctx)

		if err := b.db.TxUpdate(txn, foundRole.ID, foundRole); err != nil {
			return err
		}

		logi.Ctx(ctx).Info("role updated", slog.String("id", foundRole.ID), slog.String("name", foundRole.Name), slog.String("by", foundRole.UpdatedBy))

		return nil
	})
}

func (b *Badger) ExtendRole(txn *badger.Txn, addRoles bool, addPermissions bool, addTotalUsers bool, role *data.Role) (data.RoleExtended, error) {
	roleExtended := data.RoleExtended{
		Role: role,
	}

	if !addRoles {
		return roleExtended, nil
	}

	if addTotalUsers {
		count, err := b.db.TxCount(txn, data.User{}, badgerhold.Where("RoleIDs").Contains(role.ID).
			Or(badgerhold.Where("SyncRoleIDs").Contains(role.ID)))
		if err != nil {
			return data.RoleExtended{}, err
		}

		roleExtended.TotalUsers = count
	}

	// get roles
	if addRoles {
		var roles []data.IDName
		if err := b.db.TxForEach(txn, badgerhold.Where("ID").In(toInterfaceSlice(role.RoleIDs)...), func(role *data.Role) error {
			roles = append(roles, data.IDName{
				ID:   role.ID,
				Name: role.Name,
			})

			return nil
		}); err != nil {
			return data.RoleExtended{}, err
		}

		roleExtended.Roles = roles
	}

	// get permissions
	if addPermissions {
		var permissions []data.IDName
		if err := b.db.TxForEach(txn, badgerhold.Where("ID").In(toInterfaceSlice(role.PermissionIDs)...), func(permission *data.Permission) error {
			permissions = append(permissions, data.IDName{
				ID:   permission.ID,
				Name: permission.Name,
			})

			return nil
		}); err != nil {
			return data.RoleExtended{}, err
		}

		roleExtended.Permissions = permissions
	}

	return roleExtended, nil
}
