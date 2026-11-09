package auth

import (
	"fmt"
	"sort"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/worldline-go/types"
)

// ////////////////////////////////////////////////////////////////////
// role helpers

// virtualRoleIDs expands roles to include roles referenced by role.RoleIDs recursively.
func (sn *Snapshot) virtualRoleIDs(roleIDs []string) []string {
	mapRoleIDs := make(map[string]struct{}, len(roleIDs))
	queue := make([]string, 0, len(roleIDs))

	for _, roleID := range roleIDs {
		if _, ok := mapRoleIDs[roleID]; !ok {
			mapRoleIDs[roleID] = struct{}{}
			queue = append(queue, roleID)
		}
	}

	for len(queue) > 0 {
		roleID := queue[0]
		queue = queue[1:]

		role, ok := sn.Roles[roleID]
		if !ok {
			continue
		}

		for _, subID := range role.RoleIDs {
			if _, ok := mapRoleIDs[subID]; !ok {
				mapRoleIDs[subID] = struct{}{}
				queue = append(queue, subID)
			}
		}
	}

	result := make([]string, 0, len(mapRoleIDs))
	for roleID := range mapRoleIDs {
		result = append(result, roleID)
	}
	sort.Strings(result)

	return result
}

func userPermanentRoleSet(u *data.User) map[string]struct{} {
	set := make(map[string]struct{}, len(u.RoleIDs)+len(u.SyncRoleIDs))
	for _, id := range u.RoleIDs {
		set[id] = struct{}{}
	}
	for _, id := range u.SyncRoleIDs {
		set[id] = struct{}{}
	}

	return set
}

func userTmpRoleSet(u *data.User) map[string]struct{} {
	set := map[string]struct{}{}
	now := time.Now()
	for _, tmp := range u.TmpRoleIDs {
		if now.Before(tmp.ExpiresAt.Time) {
			set[tmp.ID] = struct{}{}
		}
	}

	return set
}

func userMixRoleSet(u *data.User) map[string]struct{} {
	set := userPermanentRoleSet(u)
	for _, id := range validIDs(u.TmpRoleIDs) {
		set[id] = struct{}{}
	}

	return set
}

func anyInSet(set map[string]struct{}, ids []string) bool {
	for _, id := range ids {
		if _, ok := set[id]; ok {
			return true
		}
	}

	return false
}

// permissionIDsByRequest returns ids of permissions matching method/path/names.
func (sn *Snapshot) permissionIDsByRequest(method, path string, names []string) []string {
	ids := make([]string, 0)

	for _, id := range sn.PermIDs {
		perm := sn.Permissions[id]

		if (method != "" || path != "") && !permissionMatchesRequest(perm, method, path) {
			continue
		}

		if len(names) > 0 && !matchAnyNameFold(perm.Name, names) {
			continue
		}

		ids = append(ids, id)
	}

	return ids
}

// roleIDsByPermissionIDs returns roles containing any of the given permission ids.
func (sn *Snapshot) roleIDsByPermissionIDs(permIDs []string) []string {
	permSet := make(map[string]struct{}, len(permIDs))
	for _, id := range permIDs {
		permSet[id] = struct{}{}
	}

	roleIDs := make([]string, 0)
	for _, id := range sn.RoleIDs {
		if anyInSet(permSet, sn.Roles[id].PermissionIDs) {
			roleIDs = append(roleIDs, id)
		}
	}

	return roleIDs
}

// ////////////////////////////////////////////////////////////////////
// users

func (sn *Snapshot) UserByAlias(alias string) *data.User {
	if id, ok := sn.Alias[normalizeAlias(alias)]; ok {
		return sn.Users[id]
	}

	return nil
}

func (sn *Snapshot) filterUsers(req data.GetUserRequest) ([]*data.User, error) {
	switch {
	case req.ID != "":
		if user, ok := sn.Users[req.ID]; ok && matchUserFlags(user, req) {
			return []*data.User{user}, nil
		}

		return nil, nil
	case req.Alias != "":
		if user := sn.UserByAlias(req.Alias); user != nil && matchUserFlags(user, req) {
			return []*data.User{user}, nil
		}

		return nil, nil
	}

	roleIDs := req.RoleIDs

	if req.Method != "" || req.Path != "" || len(req.Permissions) > 0 {
		permIDs := sn.permissionIDsByRequest(req.Method, req.Path, req.Permissions)
		if len(permIDs) == 0 {
			return nil, nil
		}

		newRoleIDs := sn.roleIDsByPermissionIDs(permIDs)
		if len(newRoleIDs) == 0 {
			return nil, nil
		}

		roleIDs = append(roleIDs, newRoleIDs...)
	}

	var expandedRoleIDs []string
	if len(roleIDs) > 0 {
		expandedRoleIDs = sn.virtualRoleIDs(roleIDs)
	}

	users := make([]*data.User, 0)
	for _, id := range sn.UserIDs {
		user := sn.Users[id]

		if !matchUserFlags(user, req) {
			continue
		}

		if req.Name != "" && !containsFold(castString(user.Details["name"]), req.Name) {
			continue
		}
		if req.Email != "" && !containsFold(castString(user.Details["email"]), req.Email) {
			continue
		}
		if req.UID != "" && !containsFold(castString(user.Details["uid"]), req.UID) {
			continue
		}
		if req.Search != "" && !matchUserSearch(user, req.Search) {
			continue
		}

		switch req.RoleType {
		case "PERMANENT":
			permanent := userPermanentRoleSet(user)
			if len(permanent) == 0 {
				continue
			}

			if len(expandedRoleIDs) > 0 && !anyInSet(permanent, expandedRoleIDs) {
				continue
			}
		case "TEMPORARY":
			tmp := userTmpRoleSet(user)
			if len(tmp) == 0 {
				continue
			}

			if len(expandedRoleIDs) > 0 && !anyInSet(tmp, expandedRoleIDs) {
				continue
			}
		default:
			if len(expandedRoleIDs) > 0 && !anyInSet(userMixRoleSet(user), expandedRoleIDs) {
				continue
			}
		}

		users = append(users, user)
	}

	return users, nil
}

func castString(v any) string {
	s, _ := v.(string)

	return s
}

// matchUserSearch is the one-box search: a case-insensitive substring match
// across every alias, description, and the name/email/uid detail fields.
func matchUserSearch(user *data.User, term string) bool {
	for _, alias := range user.Alias {
		if containsFold(alias, term) {
			return true
		}
	}
	if containsFold(user.Description, term) {
		return true
	}

	for _, key := range []string{"name", "email", "uid"} {
		if containsFold(castString(user.Details[key]), term) {
			return true
		}
	}

	return false
}

func matchUserFlags(user *data.User, req data.GetUserRequest) bool {
	if req.ServiceAccount != nil && user.ServiceAccount != *req.ServiceAccount {
		return false
	}

	if req.Disabled != nil && user.Disabled != *req.Disabled {
		return false
	}

	if req.LocalUser != nil && user.Local != *req.LocalUser {
		return false
	}

	return true
}

func (c *Cache) GetUsers(req data.GetUserRequest) (*data.Response[[]data.UserExtended], error) {
	sn := c.Snapshot()

	users, err := sn.filterUsers(req)
	if err != nil {
		return nil, err
	}

	count := uint64(len(users))

	if req.Offset > 0 {
		if int(req.Offset) >= len(users) {
			users = nil
		} else {
			users = users[req.Offset:]
		}
	}
	if req.Limit > 0 && int(req.Limit) < len(users) {
		users = users[:req.Limit]
	}

	extended := make([]data.UserExtended, 0, len(users))
	for _, user := range users {
		ext := sn.extendUser(req.AddRoles, req.AddPermissions, req.AddData, req.AddScopeRoles, user)
		ext.IsActive = !user.Disabled
		if req.Sanitize {
			ext.User = sanitizedUser(user, req.IncludeSecret)
		}
		extended = append(extended, ext)
	}

	return &data.Response[[]data.UserExtended]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: extended,
	}, nil
}

func (c *Cache) GetUser(req data.GetUserRequest) (*data.UserExtended, error) {
	sn := c.Snapshot()

	users, err := sn.filterUsers(req)
	if err != nil {
		return nil, err
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("user %s%s not found; %w", req.ID, req.Alias, data.ErrNotFound)
	}

	user := users[0]

	ext := sn.extendUser(req.AddRoles, req.AddPermissions, req.AddData, req.AddScopeRoles, user)
	ext.IsActive = !user.Disabled

	if req.Sanitize {
		ext.User = sanitizedUser(user, req.IncludeSecret)
	}

	return &ext, nil
}

func sanitizedUser(user *data.User, includeSecret bool) *data.User {
	userCopy := *user
	details := make(map[string]any, len(user.Details))
	for k, v := range user.Details {
		details[k] = v
	}

	delete(details, "password")
	details["password_set"] = user.Details["password"] != nil && user.Details["password"] != ""
	if !includeSecret {
		delete(details, "secret")
		details["secret_set"] = user.Details["secret"] != nil && user.Details["secret"] != ""
	}

	userCopy.Details = details

	return &userCopy
}

// extendUser builds UserExtended payload with roles/permissions/data/scope info.
func (sn *Snapshot) extendUser(addRoles, addRolePermissions, addData, addScopeRoles bool, user *data.User) data.UserExtended {
	userExtended := data.UserExtended{User: user}

	if !addRoles && !addRolePermissions && !addData && !addScopeRoles {
		return userExtended
	}

	mapFixedRoleIDs := userPermanentRoleSet(user)

	mapTmpRoleIDs := make(map[string]types.Time, len(user.TmpRoleIDs))
	for _, tmpRole := range validIDsWithTmpID(user.TmpRoleIDs) {
		mapTmpRoleIDs[tmpRole.ID] = tmpRole.ExpiresAt
	}

	mapTmpPermissionIDs := make(map[string]types.Time, len(user.TmpPermissionIDs))
	for _, tmpPermission := range validIDsWithTmpID(user.TmpPermissionIDs) {
		mapTmpPermissionIDs[tmpPermission.ID] = tmpPermission.ExpiresAt
	}

	mapFixedPermissionIDs := make(map[string]struct{}, len(user.PermissionIDs))
	for _, permissionID := range user.PermissionIDs {
		mapFixedPermissionIDs[permissionID] = struct{}{}
	}

	roleIDs := sn.virtualRoleIDs(slicesUnique(user.RoleIDs, user.SyncRoleIDs, validIDs(user.TmpRoleIDs)))

	var roles []data.IDName
	var permissions []data.IDName
	var rolePermissionData []any
	var scope map[string][]string

	permissionIDs := make(map[string]struct{}, 100)

	addPermission := func(permissionID string) {
		if _, ok := permissionIDs[permissionID]; ok {
			return
		}

		permission, ok := sn.Permissions[permissionID]
		if !ok {
			return
		}

		if addRolePermissions {
			permissionAdd := data.IDName{
				ID:   permission.ID,
				Name: permission.Name,
			}

			_, isFixed := mapFixedPermissionIDs[permission.ID]
			_, isTmp := mapTmpPermissionIDs[permission.ID]

			if !isFixed && isTmp {
				expiresAt := mapTmpPermissionIDs[permission.ID]
				permissionAdd.ExpiresAt = &expiresAt
			}

			if !isFixed && !isTmp {
				permissionAdd.Inherited = true
			}

			permissions = append(permissions, permissionAdd)
		}

		if addData && len(permission.Data) > 0 {
			rolePermissionData = append(rolePermissionData, permission.Data)
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

	if addRolePermissions || addData || addScopeRoles {
		for _, permissionID := range slicesUnique(user.PermissionIDs, validIDs(user.TmpPermissionIDs)) {
			addPermission(permissionID)
		}
	}

	for _, roleID := range roleIDs {
		role, ok := sn.Roles[roleID]
		if !ok {
			continue
		}

		if addRoles {
			roleAdd := data.IDName{
				ID:   role.ID,
				Name: role.Name,
			}

			_, isFixed := mapFixedRoleIDs[role.ID]
			_, isTmp := mapTmpRoleIDs[role.ID]

			if !isFixed && isTmp {
				expiresAt := mapTmpRoleIDs[role.ID]
				roleAdd.ExpiresAt = &expiresAt
			}

			if !isFixed && !isTmp {
				roleAdd.Inherited = true
			}

			roles = append(roles, roleAdd)
		}

		if addData && len(role.Data) > 0 {
			rolePermissionData = append(rolePermissionData, role.Data)
		}

		if addRolePermissions || addData || addScopeRoles {
			for _, permissionID := range role.PermissionIDs {
				addPermission(permissionID)
			}
		}
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

	return userExtended
}

// ////////////////////////////////////////////////////////////////////
// roles

func (sn *Snapshot) extendRole(addRoles, addPermissions, addTotalUsers bool, role *data.Role) data.RoleExtended {
	roleExtended := data.RoleExtended{Role: role}

	if !addRoles {
		return roleExtended
	}

	if addTotalUsers {
		var count uint64
		for _, id := range sn.UserIDs {
			if _, ok := userMixRoleSet(sn.Users[id])[role.ID]; ok {
				count++
			}
		}

		roleExtended.TotalUsers = count
	}

	var roles []data.IDName
	for _, subID := range role.RoleIDs {
		if sub, ok := sn.Roles[subID]; ok {
			roles = append(roles, data.IDName{ID: sub.ID, Name: sub.Name})
		}
	}
	roleExtended.Roles = roles

	if addPermissions {
		var permissions []data.IDName
		for _, permID := range role.PermissionIDs {
			if perm, ok := sn.Permissions[permID]; ok {
				permissions = append(permissions, data.IDName{ID: perm.ID, Name: perm.Name})
			}
		}

		roleExtended.Permissions = permissions
	}

	return roleExtended
}

func (c *Cache) GetRoles(req data.GetRoleRequest) (*data.Response[[]data.RoleExtended], error) {
	sn := c.Snapshot()

	var roles []*data.Role

	if req.ID != "" {
		if role, ok := sn.Roles[req.ID]; ok {
			roles = append(roles, role)
		}
	} else {
		permissionIDs := req.PermissionIDs
		if req.Method != "" || req.Path != "" || len(req.Permissions) > 0 {
			newIDs := sn.permissionIDsByRequest(req.Method, req.Path, req.Permissions)
			if len(newIDs) == 0 {
				return &data.Response[[]data.RoleExtended]{
					Meta:    &data.Meta{Offset: req.Offset, Limit: req.Limit},
					Payload: []data.RoleExtended{},
				}, nil
			}

			permissionIDs = append(permissionIDs, newIDs...)
		}

		permSet := make(map[string]struct{}, len(permissionIDs))
		for _, id := range permissionIDs {
			permSet[id] = struct{}{}
		}

		roleSet := make(map[string]struct{}, len(req.RoleIDs))
		for _, id := range req.RoleIDs {
			roleSet[id] = struct{}{}
		}

		for _, id := range sn.RoleIDs {
			role := sn.Roles[id]

			if req.Name != "" && !containsFold(role.Name, req.Name) {
				continue
			}

			if req.Description != "" && !containsFold(role.Description, req.Description) {
				continue
			}

			if req.Search != "" && !containsFold(role.ID, req.Search) && !containsFold(role.Name, req.Search) && !containsFold(role.Description, req.Search) {
				continue
			}

			if len(permSet) > 0 && !anyInSet(permSet, role.PermissionIDs) {
				continue
			}

			if len(roleSet) > 0 && !anyInSet(roleSet, role.RoleIDs) {
				continue
			}

			roles = append(roles, role)
		}
	}

	count := uint64(len(roles))

	if req.Offset > 0 {
		if int(req.Offset) >= len(roles) {
			roles = nil
		} else {
			roles = roles[req.Offset:]
		}
	}
	if req.Limit > 0 && int(req.Limit) < len(roles) {
		roles = roles[:req.Limit]
	}

	extended := make([]data.RoleExtended, 0, len(roles))
	for _, role := range roles {
		extended = append(extended, sn.extendRole(req.AddRoles, req.AddPermissions, req.AddTotalUsers, role))
	}

	return &data.Response[[]data.RoleExtended]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: extended,
	}, nil
}

func (c *Cache) GetRole(req data.GetRoleRequest) (*data.RoleExtended, error) {
	sn := c.Snapshot()

	role, ok := sn.Roles[req.ID]
	if !ok {
		return nil, fmt.Errorf("role with id %s not found; %w", req.ID, data.ErrNotFound)
	}

	extended := sn.extendRole(req.AddRoles, req.AddPermissions, req.AddTotalUsers, role)

	return &extended, nil
}

// ////////////////////////////////////////////////////////////////////
// permissions

func (sn *Snapshot) extendPermission(addRoles bool, permission *data.Permission) data.PermissionExtended {
	display := *permission
	display.Resources = resourcesForDisplay(permission.Resources)
	extended := data.PermissionExtended{Permission: &display}

	if !addRoles {
		return extended
	}

	var roles []data.IDName
	for _, id := range sn.RoleIDs {
		role := sn.Roles[id]
		for _, permID := range role.PermissionIDs {
			if permID == permission.ID {
				roles = append(roles, data.IDName{ID: role.ID, Name: role.Name})
				break
			}
		}
	}

	extended.Roles = roles

	return extended
}

func (c *Cache) GetPermissions(req data.GetPermissionRequest) (*data.Response[[]data.PermissionExtended], error) {
	sn := c.Snapshot()

	var permissions []*data.Permission

	if req.ID != "" {
		if permission, ok := sn.Permissions[req.ID]; ok {
			permissions = append(permissions, permission)
		}
	} else {
		for _, id := range sn.PermIDs {
			permission := sn.Permissions[id]

			if req.Name != "" && !containsFold(permission.Name, req.Name) {
				continue
			}

			if req.Description != "" && !containsFold(permission.Description, req.Description) {
				continue
			}

			if req.Search != "" && !containsFold(permission.ID, req.Search) && !containsFold(permission.Name, req.Search) && !containsFold(permission.Description, req.Search) {
				continue
			}

			if (req.Method != "" || req.Path != "") && !permissionMatchesRequest(permission, req.Method, req.Path) {
				continue
			}

			if len(req.Data) > 0 && !matchPermissionData(permission, req.Data) {
				continue
			}

			permissions = append(permissions, permission)
		}
	}

	count := uint64(len(permissions))

	if req.Offset > 0 {
		if int(req.Offset) >= len(permissions) {
			permissions = nil
		} else {
			permissions = permissions[req.Offset:]
		}
	}
	if req.Limit > 0 && int(req.Limit) < len(permissions) {
		permissions = permissions[:req.Limit]
	}

	extended := make([]data.PermissionExtended, 0, len(permissions))
	for _, permission := range permissions {
		extended = append(extended, sn.extendPermission(req.AddRoles, permission))
	}

	return &data.Response[[]data.PermissionExtended]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: extended,
	}, nil
}

func matchPermissionData(permission *data.Permission, filter map[string]string) bool {
	for k, v := range filter {
		if vv, ok := permission.Data[k]; ok {
			if containsFold(fmt.Sprint(vv), v) {
				return true
			}
		}
	}

	return false
}

func (c *Cache) GetPermission(req data.GetPermissionRequest) (*data.PermissionExtended, error) {
	sn := c.Snapshot()

	permission, ok := sn.Permissions[req.ID]
	if !ok {
		return nil, fmt.Errorf("permission with id %s not found; %w", req.ID, data.ErrNotFound)
	}

	extended := sn.extendPermission(req.AddRoles, permission)

	return &extended, nil
}

// ////////////////////////////////////////////////////////////////////
// lmaps

func (c *Cache) GetLMaps(req data.GetLMapRequest) (*data.Response[[]data.LMap], error) {
	sn := c.Snapshot()

	roleSet := make(map[string]struct{}, len(req.RoleIDs))
	for _, id := range req.RoleIDs {
		roleSet[id] = struct{}{}
	}

	lmaps := make([]data.LMap, 0, len(sn.LMapNames))
	for _, name := range sn.LMapNames {
		lmap := sn.LMaps[name]

		if req.Name != "" && lmap.Name != req.Name {
			continue
		}

		if len(roleSet) > 0 && !anyInSet(roleSet, lmap.RoleIDs) {
			continue
		}

		lmaps = append(lmaps, *lmap)
	}

	count := uint64(len(lmaps))

	if req.Offset > 0 {
		if int(req.Offset) >= len(lmaps) {
			lmaps = nil
		} else {
			lmaps = lmaps[req.Offset:]
		}
	}
	if req.Limit > 0 && int(req.Limit) < len(lmaps) {
		lmaps = lmaps[:req.Limit]
	}

	return &data.Response[[]data.LMap]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: lmaps,
	}, nil
}

func (c *Cache) GetLMap(name string) (*data.LMap, error) {
	sn := c.Snapshot()

	lmap, ok := sn.LMaps[name]
	if !ok {
		return nil, fmt.Errorf("lmap with name %s not found; %w", name, data.ErrNotFound)
	}

	return lmap, nil
}

// ////////////////////////////////////////////////////////////////////
// check

func (c *Cache) Check(req data.CheckRequest) (*data.CheckResponse, error) {
	sn := c.Snapshot()

	var user *data.User
	if req.ID != "" {
		user = sn.Users[req.ID]
	} else if req.Alias != "" {
		user = sn.UserByAlias(req.Alias)
	}

	if user == nil || user.Disabled {
		// public resources stay reachable for unknown principals; a disabled
		// user keeps individual access revoked but public is public.
		return &data.CheckResponse{Allowed: sn.checkPublic(req.Host, req.Path, req.Method)}, nil
	}

	return sn.checkUser(user, req), nil
}

// CheckPublic reports whether the request matches a permission flagged
// public; no identity involved. Used for anonymous access checks.
func (c *Cache) CheckPublic(host, path, method string) bool {
	return c.Snapshot().checkPublic(host, path, method)
}

func (sn *Snapshot) checkPublic(host, path, method string) bool {
	for _, permID := range sn.PublicPermIDs {
		perm, ok := sn.Permissions[permID]
		if !ok {
			continue
		}

		if checkAccess(sn.Check, perm, host, path, method) {
			return true
		}
	}

	return false
}

func (sn *Snapshot) checkUser(user *data.User, req data.CheckRequest) *data.CheckResponse {
	roleIDs := sn.virtualRoleIDs(slicesUnique(user.RoleIDs, user.SyncRoleIDs, validIDs(user.TmpRoleIDs)))

	permissionMapIDs := make(map[string]struct{})
	for _, permID := range user.PermissionIDs {
		permissionMapIDs[permID] = struct{}{}
	}
	for _, permID := range validIDs(user.TmpPermissionIDs) {
		permissionMapIDs[permID] = struct{}{}
	}
	for _, roleID := range roleIDs {
		if role, ok := sn.Roles[roleID]; ok {
			for _, permID := range role.PermissionIDs {
				permissionMapIDs[permID] = struct{}{}
			}
		}
	}

	for permID := range permissionMapIDs {
		perm, ok := sn.Permissions[permID]
		if !ok {
			continue
		}

		if checkAccess(sn.Check, perm, req.Host, req.Path, req.Method) {
			return &data.CheckResponse{Allowed: true}
		}
	}

	// what is public for anonymous visitors is public for users too
	if sn.checkPublic(req.Host, req.Path, req.Method) {
		return &data.CheckResponse{Allowed: true}
	}

	return &data.CheckResponse{Allowed: false}
}

// ////////////////////////////////////////////////////////////////////
// dashboard

func (c *Cache) Dashboard() (*data.Dashboard, error) {
	sn := c.Snapshot()

	var totalUsers, totalServiceAccounts uint64
	for _, id := range sn.UserIDs {
		if sn.Users[id].ServiceAccount {
			totalServiceAccounts++
		} else {
			totalUsers++
		}
	}

	extendRoles := make([]data.RoleExtended, 0, len(sn.RoleIDs))
	for _, id := range sn.RoleIDs {
		extendRoles = append(extendRoles, sn.extendRole(true, true, true, sn.Roles[id]))
	}

	return &data.Dashboard{
		Roles:                extendRoles,
		TotalRoles:           uint64(len(sn.RoleIDs)),
		TotalPermissions:     uint64(len(sn.PermIDs)),
		TotalUsers:           totalUsers,
		TotalServiceAccounts: totalServiceAccounts,
	}, nil
}
