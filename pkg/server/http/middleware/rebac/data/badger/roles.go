package badger

import (
	"errors"
	"fmt"
	"slices"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	badgerhold "github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetRoles(req data.GetRoleRequest) (*data.Response[[]data.Role], error) {
	var roles []data.Role

	badgerHoldQuery := &badgerhold.Query{}

	if req.ID != "" {
		badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID)
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
				return &data.Response[[]data.Role]{
					Meta: data.Meta{
						Offset: req.Offset,
						Limit:  req.Limit,
					},
					Payload: roles,
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

	return &data.Response[[]data.Role]{
		Meta: data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: roles,
	}, nil
}

func (b *Badger) GetRole(id string) (*data.Role, error) {
	var role data.Role

	if err := b.db.Get(id, &role); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("role with id %s not found; %w", id, data.ErrNotFound)
		}

		return nil, err
	}

	return &role, nil
}

func (b *Badger) CreateRole(role data.Role) error {
	if err := b.db.Insert(role.ID, role); err != nil {
		if errors.Is(err, badgerhold.ErrKeyExists) {
			return fmt.Errorf("role with name %s already exists; %w", role.Name, data.ErrConflict)
		}

		return err
	}

	return nil
}

func (b *Badger) PatchRole(role data.Role) error {
	var foundRole data.Role
	if err := b.db.FindOne(&foundRole, badgerhold.Where("ID").Eq(role.ID)); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("role with id %s not found; %w", role.ID, badgerhold.ErrNotFound)
		}

		return err
	}

	if role.Name != "" {
		foundRole.Name = role.Name
	}

	for _, permissionID := range role.PermissionIDs {
		if !slices.Contains(foundRole.PermissionIDs, permissionID) {
			foundRole.PermissionIDs = append(foundRole.PermissionIDs, permissionID)
		}
	}

	for _, roleID := range role.RoleIDs {
		if !slices.Contains(foundRole.RoleIDs, roleID) {
			foundRole.RoleIDs = append(foundRole.RoleIDs, roleID)
		}
	}

	if role.Data != nil {
		foundRole.Data = role.Data
	}

	if err := b.db.Update(role.ID, foundRole); err != nil {
		return err
	}

	return nil
}

func (b *Badger) PutRole(role data.Role) error {
	if err := b.db.Update(role.ID, role); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("role with id %s not found; %w", role.ID, data.ErrNotFound)
		}

		return err
	}

	return nil
}

func (b *Badger) DeleteRole(id string) error {
	if err := b.db.Delete(id, data.Role{}); err != nil {
		return err
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
