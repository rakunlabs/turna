package badger

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	"github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetUsers(req data.GetUserRequest) (*data.Response[[]data.UserExtended], error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var users []data.User

	badgerHoldQuery := &badgerhold.Query{}

	switch {
	case req.ID != "":
		badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID).Index("ID")
	case req.Alias != "":
		badgerHoldQuery = badgerhold.Where("Alias").Contains(req.Alias)
	default:
		var badgerHoldQueryInternal *badgerhold.Query
		if req.Name != "" {
			badgerHoldQueryInternal = badgerhold.Where("Details").MatchFunc(matchAllField("name", req.Name))
		}

		if req.Method != "" || req.Path != "" {
			// get role ids based on path and method
			roleIDs, err := b.getRoleIDs(req.Method, req.Path)
			if err != nil {
				return nil, err
			}

			if len(roleIDs) == 0 {
				return &data.Response[[]data.UserExtended]{
					Meta: &data.Meta{
						Offset: req.Offset,
						Limit:  req.Limit,
					},
					Payload: []data.UserExtended{},
				}, nil
			}

			req.RoleIDs = append(req.RoleIDs, roleIDs...)
		}

		if req.Email != "" {
			if badgerHoldQueryInternal != nil {
				badgerHoldQueryInternal = badgerHoldQueryInternal.And("Details").MatchFunc(matchAllField("email", req.Email))
			} else {
				badgerHoldQueryInternal = badgerhold.Where("Details").MatchFunc(matchAllField("email", req.Email))
			}
		}

		if req.UID != "" {
			if badgerHoldQueryInternal != nil {
				badgerHoldQueryInternal = badgerHoldQueryInternal.And("Details").MatchFunc(matchAllField("uid", req.UID))
			} else {
				badgerHoldQueryInternal = badgerhold.Where("Details").MatchFunc(matchAllField("uid", req.UID))
			}
		}

		if len(req.RoleIDs) > 0 {
			// role ids could be virtual roles, get all roles that contain the role ids
			roleIDs, err := b.getVirtualRoleIDs(req.RoleIDs)
			if err != nil {
				return nil, err
			}

			if badgerHoldQueryInternal != nil {
				badgerHoldQueryInternal = badgerHoldQueryInternal.And("RoleIDs").ContainsAny(toInterfaceSlice(roleIDs)...).
					Or(badgerHoldQueryInternal.And("SyncRoleIDs").ContainsAny(toInterfaceSlice(roleIDs)...))
			} else {
				badgerHoldQueryInternal = badgerhold.Where("RoleIDs").ContainsAny(toInterfaceSlice(roleIDs)...).
					Or(badgerhold.Where("SyncRoleIDs").ContainsAny(toInterfaceSlice(roleIDs)...))
			}
		}

		if badgerHoldQueryInternal != nil {
			badgerHoldQuery = badgerHoldQueryInternal
		}
	}

	if badgerHoldQuery.IsEmpty() {
		badgerHoldQuery = badgerhold.Where("ServiceAccount").Eq(req.ServiceAccount).And("Disabled").Eq(req.Disabled)
	} else {
		badgerHoldQuery = badgerHoldQuery.And("ServiceAccount").Eq(req.ServiceAccount).And("Disabled").Eq(req.Disabled)
	}

	count, err := b.db.Count(data.User{}, badgerHoldQuery)
	if err != nil {
		return nil, err
	}

	if req.Offset > 0 {
		badgerHoldQuery = badgerHoldQuery.Skip(int(req.Offset))
	}
	if req.Limit > 0 {
		badgerHoldQuery = badgerHoldQuery.Limit(int(req.Limit))
	}

	if err := b.db.Find(&users, badgerHoldQuery); err != nil {
		return nil, err
	}

	userExtended := make([]data.UserExtended, len(users))

	for i, user := range users {
		extended, err := b.extendUser(req.AddRoles, req.AddPermissions, req.AddDatas, &user)
		if err != nil {
			return nil, err
		}

		extended.IsActive = !user.Disabled

		userExtended[i] = extended
	}

	return &data.Response[[]data.UserExtended]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: userExtended,
	}, nil
}

func (b *Badger) GetCachedID(aliasName string) *data.User {
	if aliasName == "" {
		return nil
	}

	// find id in alias table
	var alias data.Alias
	if err := b.db.FindOne(&alias, badgerhold.Where("Name").Eq(aliasName)); err != nil {
		if !errors.Is(err, badgerhold.ErrNotFound) {
			slog.Error("failed to find alias cache", slog.String("error", err.Error()))
		}

		return nil
	}

	if alias.ID != "" {
		var userFind data.User
		// find user and it has same id as alias
		if err := b.db.FindOne(&userFind, badgerhold.Where("ID").Eq(alias.ID)); err != nil {
			if !errors.Is(err, badgerhold.ErrNotFound) {
				slog.Error("failed to find user in alias cache", slog.String("error", err.Error()))
			}
		} else {
			if slices.Contains(userFind.Alias, aliasName) {
				return &userFind
			}
		}
	}

	// delete alias if user not found
	if err := b.db.Delete(aliasName, data.Alias{}); err != nil {
		slog.Error("failed to delete alias cache", slog.String("error", err.Error()))
	}

	return nil
}

func (b *Badger) SetCachedID(aliasName []string, userID string) error {
	if len(aliasName) == 0 || userID == "" {
		return nil
	}

	for _, alias := range aliasName {
		// find id in alias table
		alias := data.Alias{
			ID:   userID,
			Name: alias,
		}

		if err := b.db.Upsert(alias.Name, alias); err != nil {
			return err
		}
	}

	return nil
}

func (b *Badger) GetUser(req data.GetUserRequest) (*data.UserExtended, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var user data.User

	badgerHoldQuery := &badgerhold.Query{}

	if req.ID != "" {
		badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID).Index("ID").And("ServiceAccount").Eq(req.ServiceAccount)
	} else if req.Alias != "" {
		badgerHoldQuery = badgerhold.Where("Alias").Contains(req.Alias).And("ServiceAccount").Eq(req.ServiceAccount)
	}

	if err := b.db.FindOne(&user, badgerHoldQuery); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("user with id %s not found; %w", req.ID, data.ErrNotFound)
		}

		return nil, err
	}

	extendedUser, err := b.extendUser(req.AddRoles, req.AddPermissions, req.AddDatas, &user)
	extendedUser.IsActive = !user.Disabled

	return &extendedUser, err
}

func (b *Badger) CreateUser(user data.User) (string, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	user.ID = ulid.Make().String()

	var foundUser data.User
	alias := make([]interface{}, len(user.Alias))
	for i, a := range user.Alias {
		alias[i] = a
	}

	if err := b.db.FindOne(&foundUser, badgerhold.Where("Alias").ContainsAny(alias...)); err != nil {
		if !errors.Is(err, badgerhold.ErrNotFound) {
			return "", err
		}
	}

	if foundUser.ID != "" {
		return "", fmt.Errorf("user with alias %v already exists; %w", user.Alias, data.ErrConflict)
	}

	if err := b.db.Insert(user.ID, user); err != nil {
		return "", err
	}

	if err := b.SetCachedID(user.Alias, user.ID); err != nil {
		slog.Error("failed to set alias cache", slog.String("error", err.Error()))
	}

	return user.ID, nil
}

func (b *Badger) editUser(id string, fn func(*data.User)) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var foundUser data.User
	if err := b.db.FindOne(&foundUser, badgerhold.Where("ID").Eq(id)); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("user with id %s not found; %w", id, data.ErrNotFound)
		}

		return err
	}

	fn(&foundUser)

	if err := b.db.Update(foundUser.ID, foundUser); err != nil {
		return err
	}

	return nil
}

func (b *Badger) PatchUser(id string, userPatch data.UserPatch) error {
	return b.editUser(id, func(foundUser *data.User) {
		if userPatch.Alias != nil {
			foundUser.Alias = *userPatch.Alias
		}

		if userPatch.RoleIDs != nil {
			foundUser.RoleIDs = *userPatch.RoleIDs
		}

		if userPatch.Details != nil {
			foundUser.Details = *userPatch.Details
		}

		if userPatch.SyncRoleIDs != nil {
			foundUser.SyncRoleIDs = *userPatch.SyncRoleIDs
		}

		if userPatch.IsActive != nil {
			foundUser.Disabled = !*userPatch.IsActive
		}
	})
}

func (b *Badger) PutUser(user data.User) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var foundUser data.User
	if err := b.db.FindOne(&foundUser, badgerhold.Where("ID").Eq(user.ID)); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("user with id %s not found; %w", user.ID, data.ErrNotFound)
		}

		return err
	}

	if err := b.db.Update(foundUser.ID, user); err != nil {
		return err
	}

	return nil
}

func (b *Badger) DeleteUser(id string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Delete(id, data.User{}); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("user with id %s not found; %w", id, data.ErrNotFound)
		}

		return err
	}

	// delete alias cache
	var user data.User
	if err := b.db.FindOne(&user, badgerhold.Where("ID").Eq(id)); err != nil {
		if !errors.Is(err, badgerhold.ErrNotFound) {
			return err
		}
	}

	for _, alias := range user.Alias {
		if err := b.db.Delete(alias, data.Alias{}); err != nil {
			return err
		}
	}

	return nil
}

func (b *Badger) extendUser(addRoles, addRolePermissions, addDatas bool, user *data.User) (data.UserExtended, error) {
	userExtended := data.UserExtended{
		User: user,
	}

	if !addRoles {
		return userExtended, nil
	}

	// get users roleIDs
	roleIDs, err := b.getVirtualRoleIDs(slices.Concat(user.RoleIDs, user.SyncRoleIDs))
	if err != nil {
		return data.UserExtended{}, err
	}

	var roles []data.IDName
	var permissions []data.IDName
	var datas []interface{}

	// get roles permissions
	if err := b.db.ForEach(badgerhold.Where("ID").In(toInterfaceSlice(roleIDs)...), func(role *data.Role) error {
		roles = append(roles, data.IDName{
			ID:   role.ID,
			Name: role.Name,
		})

		if addDatas {
			if role.Data != nil {
				datas = append(datas, role.Data)
			}
		}

		if addRolePermissions {
			// get permissions
			for _, permissionID := range role.PermissionIDs {
				var permission data.Permission
				if err := b.db.Get(permissionID, &permission); err != nil {
					return err
				}

				permissions = append(permissions, data.IDName{
					ID:   permission.ID,
					Name: permission.Name,
				})
			}
		}

		return nil
	}); err != nil {
		return data.UserExtended{}, err
	}

	userExtended.Roles = roles
	userExtended.Permissions = permissions
	userExtended.Datas = datas

	return userExtended, nil
}
