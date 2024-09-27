package badger

import (
	"errors"
	"fmt"
	"slices"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetRoles(req data.GetRoleRequest) (*data.Response[[]data.RoleExtended], error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var roles []data.Role

	badgerHoldQuery := &badgerhold.Query{}

	if req.ID != "" {
		badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID).Index("ID")
	} else {
		var badgerHoldQueryInternal *badgerhold.Query
		if req.Name != "" {
			badgerHoldQueryInternal = badgerhold.Where("Name").MatchFunc(matchAll(req.Name))
		}

		permissionIDs := req.PermissionIDs
		if req.Method != "" || req.Path != "" {
			// get permissions ids based on path and method
			newIDs, err := b.getPermissionIDs(req.Method, req.Path)
			if err != nil {
				return nil, err
			}

			if len(newIDs) == 0 {
				return &data.Response[[]data.RoleExtended]{
					Meta: &data.Meta{
						Offset: req.Offset,
						Limit:  req.Limit,
					},
					Payload: []data.RoleExtended{},
				}, nil
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

	count, err := b.db.Count(data.Role{}, badgerHoldQuery)
	if err != nil {
		return nil, err
	}

	if req.Offset > 0 {
		badgerHoldQuery = badgerHoldQuery.Skip(int(req.Offset))
	}
	if req.Limit > 0 {
		badgerHoldQuery = badgerHoldQuery.Limit(int(req.Limit))
	}

	if err := b.db.Find(&roles, badgerHoldQuery); err != nil {
		return nil, err
	}

	extendRoles := make([]data.RoleExtended, 0, len(roles))
	for _, role := range roles {
		extendRole, err := b.ExtendRole(req.AddRoles, req.AddPermissions, req.AddTotalUsers, &role)
		if err != nil {
			return nil, err
		}

		extendRoles = append(extendRoles, extendRole)
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

	var role data.Role

	if err := b.db.Get(req.ID, &role); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("role with id %s not found; %w", req.ID, data.ErrNotFound)
		}

		return nil, err
	}

	roleExtended, err := b.ExtendRole(req.AddRoles, req.AddPermissions, req.AddTotalUsers, &role)
	if err != nil {
		return nil, err
	}

	return &roleExtended, nil
}

func (b *Badger) CreateRole(role data.Role) (string, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	role.ID = ulid.Make().String()

	// check role with name already exists
	if err := b.db.FindOne(&data.Role{}, badgerhold.Where("Name").Eq(role.Name).Index("Name")); err != nil {
		if !errors.Is(err, badgerhold.ErrNotFound) {
			return "", err
		}
	} else {
		return "", fmt.Errorf("role with name %s already exists; %w", role.Name, data.ErrConflict)
	}

	if err := b.db.Insert(role.ID, role); err != nil {
		if errors.Is(err, badgerhold.ErrKeyExists) {
			return "", fmt.Errorf("roleID %s already exists; %w", role.ID, data.ErrConflict)
		}

		return "", err
	}

	return role.ID, nil
}

func (b *Badger) PatchRole(id string, rolePatch data.RolePatch) error {
	return b.editRole(id, func(foundRole *data.Role) error {
		if rolePatch.Name != nil && *rolePatch.Name != "" {
			// check role with name already exists
			if err := b.db.FindOne(&data.Role{}, badgerhold.Where("Name").Eq(*rolePatch.Name).Index("Name")); err != nil {
				if !errors.Is(err, badgerhold.ErrNotFound) {
					return err
				}
			} else {
				return fmt.Errorf("role with name %s already exists; %w", *rolePatch.Name, data.ErrConflict)
			}

			foundRole.Name = *rolePatch.Name
		}

		if rolePatch.Description != nil && *rolePatch.Description != "" {
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

func (b *Badger) PutRole(role data.Role) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Update(role.ID, role); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("role with id %s not found; %w", role.ID, data.ErrNotFound)
		}

		return err
	}

	return nil
}

func (b *Badger) DeleteRole(id string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Delete(id, data.Role{}); err != nil {
		return err
	}

	// Delete the role from all roles
	if err := b.db.ForEach(badgerhold.Where("RoleIDs").Contains(id), func(role *data.Role) error {
		role.RoleIDs = slices.DeleteFunc(role.RoleIDs, func(cmp string) bool {
			return cmp == id
		})

		if err := b.db.Update(role.ID, role); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete role from roles; %w", err)
	}

	// Delete the role from all users
	if err := b.db.ForEach(badgerhold.Where("RoleIDs").Contains(id), func(user *data.User) error {
		user.RoleIDs = slices.DeleteFunc(user.RoleIDs, func(cmp string) bool {
			return cmp == id
		})

		if err := b.db.Update(user.ID, user); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete role from users; %w", err)
	}

	// Delete the role from all users with sync roles
	if err := b.db.ForEach(badgerhold.Where("SyncRoleIDs").Contains(id), func(user *data.User) error {
		user.SyncRoleIDs = slices.DeleteFunc(user.SyncRoleIDs, func(cmp string) bool {
			return cmp == id
		})

		if err := b.db.Update(user.ID, user); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete role from users with sync roles; %w", err)
	}

	return nil
}

func (b *Badger) getVirtualRoleIDs(roleIDs []string) ([]string, error) {
	mapRoleIDs := make(map[string]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		mapRoleIDs[roleID] = struct{}{}
	}

	for {
		query := badgerhold.Where("ID").In(toInterfaceSlice(roleIDs)...)
		roleIDs = nil
		if err := b.db.ForEach(query, func(role *data.Role) error {
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

func (b *Badger) getRoleIDs(method, path string) ([]string, error) {
	var roleIDs []string

	permissionIDs, err := b.getPermissionIDs(method, path)
	if err != nil {
		return nil, err
	}

	query := badgerhold.Where("PermissionIDs").ContainsAny(toInterfaceSlice(permissionIDs)...)

	if err := b.db.ForEach(query, func(role *data.Role) error {
		roleIDs = append(roleIDs, role.ID)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to get role IDs; %w", err)
	}

	return roleIDs, nil
}

func (b *Badger) editRole(id string, fn func(*data.Role) error) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var foundRole data.Role
	if err := b.db.FindOne(&foundRole, badgerhold.Where("ID").Eq(id).Index("ID")); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("role with id %s not found; %w", id, data.ErrNotFound)
		}

		return err
	}

	if err := fn(&foundRole); err != nil {
		return err
	}

	if err := b.db.Update(foundRole.ID, foundRole); err != nil {
		return err
	}

	return nil
}

func (b *Badger) ExtendRole(addRoles bool, addPermissions bool, addTotalUsers bool, role *data.Role) (data.RoleExtended, error) {
	roleExtended := data.RoleExtended{
		Role: role,
	}

	if !addRoles {
		return roleExtended, nil
	}

	if addTotalUsers {
		count, err := b.db.Count(data.User{}, badgerhold.Where("RoleIDs").Contains(role.ID).
			Or(badgerhold.Where("SyncRoleIDs").Contains(role.ID)))
		if err != nil {
			return data.RoleExtended{}, err
		}

		roleExtended.TotalUsers = count
	}

	// get roles
	if addRoles {
		var roles []string
		if err := b.db.ForEach(badgerhold.Where("ID").In(toInterfaceSlice(role.RoleIDs)...), func(role *data.Role) error {
			roles = append(roles, role.Name)
			return nil
		}); err != nil {
			return data.RoleExtended{}, err
		}

		roleExtended.Roles = roles
	}

	// get permissions
	if addPermissions {
		var permissions []string
		if err := b.db.ForEach(badgerhold.Where("ID").In(toInterfaceSlice(role.PermissionIDs)...), func(permission *data.Permission) error {
			permissions = append(permissions, permission.Name)
			return nil
		}); err != nil {
			return data.RoleExtended{}, err
		}

		roleExtended.Permissions = permissions
	}

	return roleExtended, nil
}
