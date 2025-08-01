package badger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/logi"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/access"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/spf13/cast"
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

		if req.ServiceAccount != nil {
			if badgerHoldQuery.IsEmpty() {
				badgerHoldQuery = badgerhold.Where("ServiceAccount").Eq(*req.ServiceAccount)
			} else {
				badgerHoldQuery = badgerHoldQuery.And("ServiceAccount").Eq(*req.ServiceAccount)
			}
		}

		if req.Disabled != nil {
			if badgerHoldQuery.IsEmpty() {
				badgerHoldQuery = badgerhold.Where("Disabled").Eq(*req.Disabled)
			} else {
				badgerHoldQuery = badgerHoldQuery.And("Disabled").Eq(*req.Disabled)
			}
		}

		switch {
		case req.ID != "":
			if badgerHoldQuery.IsEmpty() {
				badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID).Index("ID")
			} else {
				badgerHoldQuery = badgerHoldQuery.And("ID").Eq(req.ID).Index("ID")
			}
		case req.Alias != "":
			if badgerHoldQuery.IsEmpty() {
				badgerHoldQuery = badgerhold.Where("Alias").Contains(req.Alias)
			} else {
				badgerHoldQuery = badgerHoldQuery.And("Alias").Contains(req.Alias)
			}
		default:
			if req.Name != "" {
				if badgerHoldQuery.IsEmpty() {
					badgerHoldQuery = badgerhold.Where("Details").MatchFunc(matchAllField("name", req.Name))
				} else {
					badgerHoldQuery = badgerHoldQuery.And("Details").MatchFunc(matchAllField("name", req.Name))
				}
			}

			if req.Method != "" || req.Path != "" || len(req.Permissions) > 0 {
				// get role ids based on path and method
				roleIDs, err := b.getRoleIDs(txn, req.Method, req.Path, req.Permissions)
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
				if badgerHoldQuery.IsEmpty() {
					badgerHoldQuery = badgerhold.Where("Details").MatchFunc(matchAllField("email", req.Email))
				} else {
					badgerHoldQuery = badgerHoldQuery.And("Details").MatchFunc(matchAllField("email", req.Email))
				}
			}

			if req.UID != "" {
				if badgerHoldQuery.IsEmpty() {
					badgerHoldQuery = badgerhold.Where("Details").MatchFunc(matchAllField("uid", req.UID))
				} else {
					badgerHoldQuery = badgerHoldQuery.And("Details").MatchFunc(matchAllField("uid", req.UID))
				}
			}

			if len(req.RoleIDs) > 0 {
				// role ids could be virtual roles, get all roles that contain the role ids
				roleIDs, err := b.getVirtualRoleIDs(txn, req.RoleIDs)
				if err != nil {
					return err
				}

				if badgerHoldQuery.IsEmpty() {
					badgerHoldQuery = badgerhold.Where("RoleIDs").ContainsAny(toInterfaceSlice(roleIDs)...).
						Or(badgerhold.Where("SyncRoleIDs").ContainsAny(toInterfaceSlice(roleIDs)...)).
						Or(badgerhold.Where("TmpRoleIDs").MatchFunc(matchTmpIDWithCheck(roleIDs...)))
				} else {
					badgerHoldQuery = badgerHoldQuery.And("RoleIDs").ContainsAny(toInterfaceSlice(roleIDs)...).
						Or(badgerHoldQuery.And("SyncRoleIDs").ContainsAny(toInterfaceSlice(roleIDs)...)).
						Or(badgerHoldQuery.And("TmpRoleIDs").MatchFunc(matchTmpIDWithCheck(roleIDs...)))
				}
			}
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
			extended, err := b.extendUser(txn, req.AddRoles, req.AddPermissions, req.AddData, req.AddScopeRoles, &user)
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

func (b *Badger) TxGetUser(txn *badger.Txn, req data.GetUserRequest) (*data.UserExtended, error) {
	var extendedUser data.UserExtended

	var err error
	var user data.User

	badgerHoldQuery := &badgerhold.Query{}

	if req.ID != "" {
		badgerHoldQuery = badgerhold.Where("ID").Eq(req.ID).Index("ID")
	} else if req.Alias != "" {
		badgerHoldQuery = badgerhold.Where("Alias").Contains(req.Alias)
	}

	if req.ServiceAccount != nil {
		if badgerHoldQuery.IsEmpty() {
			badgerHoldQuery = badgerhold.Where("ServiceAccount").Eq(*req.ServiceAccount)
		} else {
			badgerHoldQuery = badgerHoldQuery.And("ServiceAccount").Eq(*req.ServiceAccount)
		}
	}

	if req.LocalUser != nil {
		if badgerHoldQuery.IsEmpty() {
			badgerHoldQuery = badgerhold.Where("Local").Eq(*req.LocalUser)
		} else {
			badgerHoldQuery = badgerHoldQuery.And("Local").Eq(*req.LocalUser)
		}
	}

	if err := b.db.TxFindOne(txn, &user, badgerHoldQuery); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return nil, fmt.Errorf("user with id %s not found; %w", req.ID, data.ErrNotFound)
		}

		return nil, err
	}

	extendedUser, err = b.extendUser(txn, req.AddRoles, req.AddPermissions, req.AddData, req.AddScopeRoles, &user)
	if err != nil {
		return nil, err
	}
	extendedUser.IsActive = !user.Disabled

	if req.Sanitize {
		for _, v := range []string{"password", "secret"} {
			delete(extendedUser.User.Details, v)
		}
	}

	return &extendedUser, nil
}

func (b *Badger) TxFuncUser(txn *badger.Txn, req data.GetUserRequest, fn func(*data.User) (*data.User, error)) error {
	badgerHoldQuery := &badgerhold.Query{}

	if req.ServiceAccount != nil {
		if badgerHoldQuery.IsEmpty() {
			badgerHoldQuery = badgerhold.Where("ServiceAccount").Eq(*req.ServiceAccount)
		} else {
			badgerHoldQuery = badgerHoldQuery.And("ServiceAccount").Eq(*req.ServiceAccount)
		}
	}

	if req.LocalUser != nil {
		if badgerHoldQuery.IsEmpty() {
			badgerHoldQuery = badgerhold.Where("Local").Eq(*req.LocalUser)
		} else {
			badgerHoldQuery = badgerHoldQuery.And("Local").Eq(*req.LocalUser)
		}
	}

	return b.db.TxForEach(txn, badgerHoldQuery, func(user *data.User) error {
		userNew, err := fn(user)
		if err != nil {
			return err
		}

		if userNew == nil {
			return nil
		}

		if err := b.db.TxUpdate(txn, userNew.ID, userNew); err != nil {
			return err
		}

		return nil
	})
}

func (b *Badger) GetUser(req data.GetUserRequest) (*data.UserExtended, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var extendedUser *data.UserExtended

	if err := b.db.Badger().View(func(txn *badger.Txn) error {
		var err error
		extendedUser, err = b.TxGetUser(txn, req)
		return err
	}); err != nil {
		return nil, err
	}

	return extendedUser, nil
}

func (b *Badger) TxCreateUser(ctx context.Context, txn *badger.Txn, user data.User) (string, error) {
	var foundUser data.User

	user.ID = ulid.Make().String()

	if err := b.db.TxFindOne(txn, &foundUser, badgerhold.Where("Alias").ContainsAny(toInterfaceSlice(user.Alias)...)); err != nil {
		if !errors.Is(err, badgerhold.ErrNotFound) {
			return "", err
		}
	}

	if foundUser.ID != "" {
		return "", fmt.Errorf("user with alias %v already exists; %w", user.Alias, data.ErrConflict)
	}

	if user.Details != nil {
		if v := cast.ToString(user.Details["password"]); v != "" {
			hashPassword, err := access.ToBcrypt([]byte(v))
			if err != nil {
				slog.Error("Cannot hash password", slog.String("error", err.Error()))
			}

			user.Details["password"] = hashPassword
		}
	}

	user.RoleIDs = slicesUnique(user.RoleIDs)
	user.SyncRoleIDs = slicesUnique(user.SyncRoleIDs)

	user.CreatedAt = time.Now().Format(time.RFC3339)
	user.UpdatedAt = user.CreatedAt
	user.UpdatedBy = data.CtxUserName(ctx)

	if err := b.db.TxInsert(txn, user.ID, user); err != nil {
		return "", err
	}

	if err := b.TxSetCachedID(txn, user.Alias, user.ID); err != nil {
		slog.Error("failed to set alias cache", slog.String("error", err.Error()))
	}

	msg := "user created"
	if user.ServiceAccount {
		msg = "service account created"
	}

	logi.Ctx(ctx).Info(msg, slog.String("id", user.ID), slog.String("alias", strings.Join(user.Alias, ",")), slog.String("by", user.UpdatedBy))

	return user.ID, nil
}

func (b *Badger) CreateUser(ctx context.Context, user data.User) (string, error) {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	var id string

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
		var err error
		id, err = b.TxCreateUser(ctx, txn, user)
		return err
	}); err != nil {
		return id, err
	}

	return id, nil
}

func (b *Badger) editUser(ctx context.Context, id string, fn func(*badger.Txn, *data.User)) error {
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

		userName := data.CtxUserName(ctx)

		foundUser.UpdatedAt = time.Now().Format(time.RFC3339)
		foundUser.UpdatedBy = userName

		if err := b.db.TxUpdate(txn, foundUser.ID, foundUser); err != nil {
			return err
		}

		msg := "user updated"
		if foundUser.ServiceAccount {
			msg = "service account updated"
		}

		logi.Ctx(ctx).Info(msg, slog.String("id", id), slog.String("alias", strings.Join(foundUser.Alias, ",")), slog.String("by", userName))

		return nil
	})
}

func (b *Badger) PatchUser(ctx context.Context, id string, userPatch data.UserPatch) error {
	return b.editUser(ctx, id, func(_ *badger.Txn, foundUser *data.User) {
		if userPatch.Alias != nil {
			foundUser.Alias = *userPatch.Alias
		}

		if userPatch.Details != nil {
			if v := cast.ToString((*userPatch.Details)["password"]); v != "" {
				hashPassword, err := access.ToBcrypt([]byte(v))
				if err != nil {
					slog.Error("Cannot hash password", slog.String("error", err.Error()))
				}

				(*userPatch.Details)["password"] = hashPassword
			}

			if foundUser.Details != nil && foundUser.Details["password"] != nil && (*userPatch.Details)["password"] == nil {
				(*userPatch.Details)["password"] = foundUser.Details["password"]
			}

			foundUser.Details = *userPatch.Details
		}

		if userPatch.PermissionIDs != nil {
			foundUser.PermissionIDs = slicesUnique(*userPatch.PermissionIDs)
		}

		if userPatch.RoleIDs != nil {
			foundUser.RoleIDs = slicesUnique(*userPatch.RoleIDs)
		}

		if userPatch.SyncRoleIDs != nil {
			foundUser.SyncRoleIDs = slicesUnique(*userPatch.SyncRoleIDs)
		}

		if userPatch.IsActive != nil {
			foundUser.Disabled = !*userPatch.IsActive
		}
	})
}

func (b *Badger) PatchUserAccess(ctx context.Context, id string, userAccess data.UserAccess) error {
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

	return b.editUser(ctx, id, func(_ *badger.Txn, foundUser *data.User) {
		if expires == nil {
			// remove access
			newTmpRoleIDs := make([]data.TmpID, 0, len(foundUser.TmpRoleIDs))
			for _, tmpRole := range foundUser.TmpRoleIDs {
				if _, ok := roleIDMap[tmpRole.ID]; !ok {
					newTmpRoleIDs = append(newTmpRoleIDs, tmpRole)
				}
			}
			foundUser.TmpRoleIDs = newTmpRoleIDs

			newTmpPermissionIDs := make([]data.TmpID, 0, len(foundUser.TmpPermissionIDs))
			for _, tmpPermission := range foundUser.TmpPermissionIDs {
				if _, ok := permissionIDMap[tmpPermission.ID]; !ok {
					newTmpPermissionIDs = append(newTmpPermissionIDs, tmpPermission)
				}
			}

			foundUser.TmpPermissionIDs = newTmpPermissionIDs

			return
		}

		for tmpRole := range roleIDMap {
			// replace existing tmp role
			index := slices.IndexFunc(foundUser.TmpRoleIDs, func(existingTmpRole data.TmpID) bool {
				return existingTmpRole.ID == tmpRole
			})

			if index == -1 {
				// add new tmp role
				foundUser.TmpRoleIDs = append(foundUser.TmpRoleIDs, data.TmpID{
					ID:        tmpRole,
					ExpiresAt: *expires,
				})

				continue
			}

			// update existing tmp role
			foundUser.TmpRoleIDs[index].ExpiresAt = *expires
		}

		for tmpPermission := range permissionIDMap {
			// replace existing tmp permission
			index := slices.IndexFunc(foundUser.TmpPermissionIDs, func(existingTmpPermission data.TmpID) bool {
				return existingTmpPermission.ID == tmpPermission
			})

			if index == -1 {
				// add new tmp permission
				foundUser.TmpPermissionIDs = append(foundUser.TmpPermissionIDs, data.TmpID{
					ID:        tmpPermission,
					ExpiresAt: *expires,
				})

				continue
			}

			// update existing tmp permission
			foundUser.TmpPermissionIDs[index].ExpiresAt = *expires
		}
	})
}

func (b *Badger) TxPutUser(ctx context.Context, txn *badger.Txn, user data.User) error {
	var foundUser data.User
	if err := b.db.TxFindOne(txn, &foundUser, badgerhold.Where("ID").Eq(user.ID)); err != nil {
		if errors.Is(err, badgerhold.ErrNotFound) {
			return fmt.Errorf("user with id %s not found; %w", user.ID, data.ErrNotFound)
		}

		return err
	}

	if user.Details != nil {
		if v := cast.ToString(user.Details["password"]); v != "" {
			hashPassword, err := access.ToBcrypt([]byte(v))
			if err != nil {
				slog.Error("Cannot hash password", slog.String("error", err.Error()))
			}

			user.Details["password"] = hashPassword
		}
	}

	user.RoleIDs = slicesUnique(user.RoleIDs)
	user.SyncRoleIDs = slicesUnique(user.SyncRoleIDs)
	user.UpdatedAt = time.Now().Format(time.RFC3339)
	user.CreatedAt = foundUser.CreatedAt
	user.UpdatedBy = data.CtxUserName(ctx)

	if err := b.db.TxUpdate(txn, foundUser.ID, user); err != nil {
		return err
	}

	msg := "user replaced"
	if user.ServiceAccount {
		msg = "service account replaced"
	}

	logi.Ctx(ctx).Info(msg, slog.String("id", user.ID), slog.String("alias", strings.Join(user.Alias, ",")), slog.String("by", user.UpdatedBy))

	return nil
}

func (b *Badger) PutUser(ctx context.Context, user data.User) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	return b.db.Badger().Update(func(txn *badger.Txn) error {
		return b.TxPutUser(ctx, txn, user)
	})
}

func (b *Badger) DeleteUser(ctx context.Context, id string) error {
	b.dbBackupLock.RLock()
	defer b.dbBackupLock.RUnlock()

	if err := b.db.Badger().Update(func(txn *badger.Txn) error {
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
	}); err != nil {
		return err
	}

	logi.Ctx(ctx).Info("deleted user", slog.String("id", id), slog.String("by", data.CtxUserName(ctx)))

	return nil
}

func (b *Badger) extendUser(txn *badger.Txn, addRoles, addRolePermissions, addData, addScopeRoles bool, user *data.User) (data.UserExtended, error) {
	userExtended := data.UserExtended{
		User: user,
	}

	if !addRoles && !addRolePermissions && !addData && !addScopeRoles {
		return userExtended, nil
	}

	// get users roleIDs
	roleIDs, err := b.getVirtualRoleIDs(txn, slicesUnique(user.RoleIDs, user.SyncRoleIDs, validIDs(user.TmpRoleIDs)))
	if err != nil {
		return data.UserExtended{}, err
	}

	var roles []data.IDName
	var permissions []data.IDName
	var rolePermissionData []interface{}
	var scope map[string][]string

	permissionIDs := make(map[string]struct{}, 100)

	if addRolePermissions || addData || addScopeRoles {
		// get permissions
		for _, permissionID := range slicesUnique(user.PermissionIDs, validIDs(user.TmpPermissionIDs)) {
			if _, ok := permissionIDs[permissionID]; ok {
				continue
			}

			var permission data.Permission
			if err := b.db.TxGet(txn, permissionID, &permission); err != nil {
				return data.UserExtended{}, err
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

			if addScopeRoles {
				if scope == nil {
					scope = make(map[string][]string)
				}

				for s, v := range permission.Scope {
					scope[s] = append(scope[s], v...)
				}
			}

			permissionIDs[permissionID] = struct{}{}
		}
	}

	// get roles permissions
	if err := b.db.TxForEach(txn, badgerhold.Where("ID").In(toInterfaceSlice(roleIDs)...), func(role *data.Role) error {
		if addRoles {
			roles = append(roles, data.IDName{
				ID:   role.ID,
				Name: role.Name,
			})
		}

		if addData {
			if len(role.Data) > 0 {
				rolePermissionData = append(rolePermissionData, role.Data)
			}
		}

		if addRolePermissions || addData || addScopeRoles {
			// get permissions
			for _, permissionID := range role.PermissionIDs {
				if _, ok := permissionIDs[permissionID]; ok {
					continue
				}

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

				if addScopeRoles {
					if scope == nil {
						scope = make(map[string][]string)
					}

					for s, v := range permission.Scope {
						scope[s] = append(scope[s], v...)
					}
				}

				permissionIDs[permissionID] = struct{}{}
			}
		}

		return nil
	}); err != nil {
		return data.UserExtended{}, err
	}

	for s, v := range scope {
		scope[s] = slicesUnique(v)
	}

	userExtended.Roles = roles
	userExtended.Permissions = permissions
	userExtended.Data = rolePermissionData

	if addScopeRoles {
		userExtended.Scope = scope
	}

	return userExtended, nil
}
