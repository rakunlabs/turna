package badger

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/dgraph-io/badger/v4"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/timshannon/badgerhold/v4"
)

func (b *Badger) GetUsers(req data.GetUserRequest) (*data.Response[[]data.UserExtended], error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var count uint64
	var userExtended []data.UserExtended

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		var err error
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
				roleIDs, err := b.getRoleIDs(txn, req.Method, req.Path)
				if err != nil {
					return err
				}

				if len(roleIDs) == 0 {
					userExtended = []data.UserExtended{}
					return nil
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
				roleIDs, err := b.getVirtualRoleIDs(txn, req.RoleIDs)
				if err != nil {
					return err
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
			if req.ServiceAccount != nil {
				badgerHoldQuery = badgerhold.Where("ServiceAccount").Eq(*req.ServiceAccount)
			} else {
				badgerHoldQuery = badgerhold.Where("ID").Ne("").Index("ID")
			}
		} else {
			if req.ServiceAccount != nil {
				badgerHoldQuery = badgerHoldQuery.And("ServiceAccount").Eq(*req.ServiceAccount)
			}
		}

		if req.Disabled != nil {
			badgerHoldQuery = badgerHoldQuery.And("Disabled").Eq(*req.Disabled)
		}

		count, err = b.db.TxCount(txn, data.User{}, badgerHoldQuery)
		if err != nil {
			return err
		}

		if req.Offset > 0 {
			badgerHoldQuery = badgerHoldQuery.Skip(int(req.Offset))
		}
		if req.Limit > 0 {
			badgerHoldQuery = badgerHoldQuery.Limit(int(req.Limit))
		}

		if err := b.db.TxFind(txn, &users, badgerHoldQuery); err != nil {
			return err
		}

		userExtended = make([]data.UserExtended, len(users))

		for i, user := range users {
			extended, err := b.extendUser(txn, req.AddRoles, req.AddPermissions, req.AddData, &user)
			if err != nil {
				return err
			}

			extended.IsActive = !user.Disabled

			userExtended[i] = extended
		}

		return nil
	}); err != nil {
		return nil, err
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

func (b *Badger) getCachedID(txn *badger.Txn, aliasName string) *data.User {
	if aliasName == "" {
		return nil
	}

	// find id in alias table
	var alias data.Alias
	if err := b.db.TxFindOne(txn, &alias, badgerhold.Where("Name").Eq(aliasName)); err != nil {
		if !errors.Is(err, badgerhold.ErrNotFound) {
			slog.Error("failed to find alias cache", slog.String("error", err.Error()))
		}

		return nil
	}

	if alias.ID != "" {
		var userFind data.User
		// find user and it has same id as alias
		if err := b.db.TxFindOne(txn, &userFind, badgerhold.Where("ID").Eq(alias.ID)); err != nil {
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
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Delete(aliasName, data.Alias{}); err != nil {
		slog.Error("failed to delete alias cache", slog.String("error", err.Error()))
	}

	return nil
}

func (b *Badger) SetCachedID(aliasName []string, userID string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().Update(func(txn *badger.Txn) error {
		return b.TxSetCachedID(txn, aliasName, userID)
	})
}

func (b *Badger) TxSetCachedID(txn *badger.Txn, aliasName []string, userID string) error {
	if len(aliasName) == 0 || userID == "" {
		return nil
	}

	for _, alias := range aliasName {
		// find id in alias table
		alias := data.Alias{
			ID:   userID,
			Name: alias,
		}

		if err := b.db.TxUpsert(txn, alias.Name, alias); err != nil {
			return err
		}
	}

	return nil
}

func (b *Badger) GetUser(req data.GetUserRequest) (*data.UserExtended, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var extendedUser data.UserExtended

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		var err error
		var user data.User

		badgerHoldQuery := &badgerhold.Query{}

		if req.ID != "" {
			badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID).Index("ID")
		} else if req.Alias != "" {
			badgerHoldQuery = badgerhold.Where("Alias").Contains(req.Alias)
		}

		if req.ServiceAccount != nil {
			badgerHoldQuery = badgerHoldQuery.And("ServiceAccount").Eq(*req.ServiceAccount)
		}

		if err := b.db.TxFindOne(txn, &user, badgerHoldQuery); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("user with id %s not found; %w", req.ID, data.ErrNotFound)
			}

			return err
		}

		extendedUser, err = b.extendUser(txn, req.AddRoles, req.AddPermissions, req.AddData, &user)
		if err != nil {
			return err
		}
		extendedUser.IsActive = !user.Disabled

		return nil
	}); err != nil {
		return nil, err
	}

	if req.Sanitize {
		for _, v := range []string{"password", "secret"} {
			delete(extendedUser.User.Details, v)
		}
	}

	return &extendedUser, nil
}

func (b *Badger) CreateUser(user data.User) (string, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	user.ID = ulid.Make().String()

	var foundUser data.User

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		if err := b.db.TxFindOne(txn, &foundUser, badgerhold.Where("Alias").ContainsAny(toInterfaceSlice(user.Alias)...)); err != nil {
			if !errors.Is(err, badgerhold.ErrNotFound) {
				return err
			}
		}

		if foundUser.ID != "" {
			return fmt.Errorf("user with alias %v already exists; %w", user.Alias, data.ErrConflict)
		}

		if err := b.db.TxInsert(txn, user.ID, user); err != nil {
			return err
		}

		if err := b.TxSetCachedID(txn, user.Alias, user.ID); err != nil {
			slog.Error("failed to set alias cache", slog.String("error", err.Error()))
		}

		return nil
	}); err != nil {
		return "", err
	}

	return user.ID, nil
}

func (b *Badger) editUser(id string, fn func(*badger.Txn, *data.User)) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().Update(func(txn *badger.Txn) error {
		var foundUser data.User
		if err := b.db.TxFindOne(txn, &foundUser, badgerhold.Where("ID").Eq(id)); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("user with id %s not found; %w", id, data.ErrNotFound)
			}

			return err
		}

		fn(txn, &foundUser)

		if err := b.db.TxUpdate(txn, foundUser.ID, foundUser); err != nil {
			return err
		}

		return nil
	})
}

func (b *Badger) PatchUser(id string, userPatch data.UserPatch) error {
	return b.editUser(id, func(_ *badger.Txn, foundUser *data.User) {
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

	return b.db.Badger().Update(func(txn *badger.Txn) error {
		var foundUser data.User
		if err := b.db.TxFindOne(txn, &foundUser, badgerhold.Where("ID").Eq(user.ID)); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("user with id %s not found; %w", user.ID, data.ErrNotFound)
			}

			return err
		}

		if err := b.db.TxUpdate(txn, foundUser.ID, user); err != nil {
			return err
		}

		return nil
	})
}

func (b *Badger) DeleteUser(id string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().Update(func(txn *badger.Txn) error {
		if err := b.db.TxDelete(txn, id, data.User{}); err != nil {
			if errors.Is(err, badgerhold.ErrNotFound) {
				return fmt.Errorf("user with id %s not found; %w", id, data.ErrNotFound)
			}

			return err
		}

		// delete alias cache
		var user data.User
		if err := b.db.TxFindOne(txn, &user, badgerhold.Where("ID").Eq(id)); err != nil {
			if !errors.Is(err, badgerhold.ErrNotFound) {
				return err
			}
		}

		for _, alias := range user.Alias {
			if err := b.db.TxDelete(txn, alias, data.Alias{}); err != nil {
				return err
			}
		}

		return nil
	})
}

func (b *Badger) extendUser(txn *badger.Txn, addRoles, addRolePermissions, addData bool, user *data.User) (data.UserExtended, error) {
	userExtended := data.UserExtended{
		User: user,
	}

	if !addRoles {
		return userExtended, nil
	}

	// get users roleIDs
	roleIDs, err := b.getVirtualRoleIDs(txn, slices.Concat(user.RoleIDs, user.SyncRoleIDs))
	if err != nil {
		return data.UserExtended{}, err
	}

	var roles []data.IDName
	var permissions []data.IDName
	var rolePermissionData []interface{}

	// get roles permissions
	if err := b.db.TxForEach(txn, badgerhold.Where("ID").In(toInterfaceSlice(roleIDs)...), func(role *data.Role) error {
		roles = append(roles, data.IDName{
			ID:   role.ID,
			Name: role.Name,
		})

		if addData {
			if len(role.Data) > 0 {
				rolePermissionData = append(rolePermissionData, role.Data)
			}
		}

		if addRolePermissions || addData {
			// get permissions
			for _, permissionID := range role.PermissionIDs {
				var permission data.Permission
				if err := b.db.TxGet(txn, permissionID, &permission); err != nil {
					return err
				}

				if addRolePermissions {
					permissions = append(permissions, data.IDName{
						ID:   permission.ID,
						Name: permission.Name,
					})
				}

				if addData {
					if len(permission.Data) > 0 {
						rolePermissionData = append(rolePermissionData, permission.Data)
					}
				}
			}
		}

		return nil
	}); err != nil {
		return data.UserExtended{}, err
	}

	userExtended.Roles = roles
	userExtended.Permissions = permissions
	userExtended.Data = rolePermissionData

	return userExtended, nil
}
