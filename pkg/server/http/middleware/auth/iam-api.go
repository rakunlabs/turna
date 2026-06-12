package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/query"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

type ctxKeySA struct{}

type userPutRequest struct {
	data.User
	IsActive *bool `json:"is_active"`
}

func wrapServiceAccount(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeySA{}, true))
}

func isServiceAccount(r *http.Request) bool {
	v, _ := r.Context().Value(ctxKeySA{}).(bool)

	return v
}

func isServiceAccountPtr(r *http.Request) *bool {
	v := isServiceAccount(r)

	return &v
}

func userCtx(r *http.Request) context.Context {
	return data.WithContextUserName(r.Context(), getUserName(r))
}

// parseListQuery parses the raw query string with the rakunlabs/query syntax
// (_limit, _offset, _sort, field filters). Invalid input falls back to an
// empty query so list endpoints keep working.
func parseListQuery(r *http.Request) *query.Query {
	q, err := httputil.ParseQuery(r)
	if err != nil {
		return query.New()
	}

	return q
}

// getLimitOffset reads _limit/_offset with fallback to legacy limit/offset keys.
func getLimitOffset(q *query.Query) (limit, offset int64) {
	limit = int64(q.GetLimit())   //nolint:gosec // G115
	offset = int64(q.GetOffset()) //nolint:gosec // G115

	if limit == 0 {
		limit, _ = strconv.ParseInt(q.GetValue("limit"), 10, 64)
	}
	if offset == 0 {
		offset, _ = strconv.ParseInt(q.GetValue("offset"), 10, 64)
	}

	return limit, offset
}

func parseBoolDefault(q *query.Query, key string, def bool) bool {
	if raw := q.GetValue(key); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			return parsed
		}
	}

	return def
}

func handleIAMError(w http.ResponseWriter, err error, msg string) {
	switch {
	case errors.Is(err, data.ErrNotFound):
		httputil.HandleError(w, httputil.NewError(msg, err, http.StatusNotFound))
	case errors.Is(err, data.ErrConflict):
		httputil.HandleError(w, httputil.NewError(msg, err, http.StatusConflict))
	case errors.Is(err, data.ErrInvalidRequest):
		httputil.HandleError(w, httputil.NewError(msg, err, http.StatusBadRequest))
	default:
		httputil.HandleError(w, httputil.NewError(msg, err, http.StatusInternalServerError))
	}
}

func (m *Auth) reload(r *http.Request) {
	if err := m.cache.Reload(r.Context()); err != nil {
		// reload failure is recoverable through version polling
		slog.Error("auth cache reload failed", slog.String("error", err.Error()))
	}
}

// ////////////////////////////////////////////////////////////////////
// users

func parseUserQuery(r *http.Request) data.GetUserRequest {
	q := parseListQuery(r)

	req := data.GetUserRequest{
		ID:             q.GetValue("id"),
		Alias:          q.GetValue("alias"),
		Name:           q.GetValue("name"),
		Email:          q.GetValue("email"),
		UID:            q.GetValue("uid"),
		Path:           q.GetValue("path"),
		Method:         q.GetValue("method"),
		RoleType:       q.GetValue("role_type"),
		RoleIDs:        q.GetValues("role_ids"),
		Permissions:    q.GetValues("permission"),
		ServiceAccount: isServiceAccountPtr(r),
		AddRoles:       parseBoolDefault(q, "add_roles", true),
		AddPermissions: parseBoolDefault(q, "add_permissions", false),
		AddData:        parseBoolDefault(q, "add_data", false),
		AddScopeRoles:  parseBoolDefault(q, "add_scope_roles", false),
	}

	if v := q.GetValue("is_active"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			disabled := !parsed
			req.Disabled = &disabled
		}
	}

	req.Limit, req.Offset = getLimitOffset(q)

	return req
}

func (m *Auth) GetUsersAPI(w http.ResponseWriter, r *http.Request) {
	users, err := m.cache.GetUsers(parseUserQuery(r))
	if err != nil {
		handleIAMError(w, err, "cannot get users")
		return
	}

	httputil.JSON(w, http.StatusOK, users)
}

func (m *Auth) ExportUsersAPI(w http.ResponseWriter, r *http.Request) {
	req := parseUserQuery(r)
	req.AddRoles = true
	req.Limit = 0
	req.Offset = 0

	users, err := m.cache.GetUsers(req)
	if err != nil {
		handleIAMError(w, err, "cannot export users")
		return
	}

	headers := []string{"UID", "Name", "Email", "Roles", "Is Active"}
	rows := make([]map[string]any, 0, len(users.Payload))
	for _, user := range users.Payload {
		details := user.Details
		if details == nil {
			details = map[string]any{}
		}

		roles := make([]string, 0, len(user.Roles))
		for _, role := range user.Roles {
			roles = append(roles, role.Name)
		}

		rows = append(rows, map[string]any{
			"UID":       details["uid"],
			"Name":      details["name"],
			"Email":     details["email"],
			"Roles":     strings.Join(roles, ","),
			"Is Active": user.IsActive,
		})
	}

	fileName := "users_" + time.Now().Format("20060102T1504") + ".csv"
	if isServiceAccount(r) {
		fileName = "service_" + time.Now().Format("20060102T1504") + ".csv"
	}

	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, rows, fileName); err != nil {
		handleIAMError(w, err, "cannot export users")
	}
}

func (m *Auth) GetUserAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)

	req := data.GetUserRequest{
		ID:             r.PathValue("id"),
		ServiceAccount: isServiceAccountPtr(r),
		AddRoles:       parseBoolDefault(q, "add_roles", true),
		AddPermissions: parseBoolDefault(q, "add_permissions", false),
		AddData:        parseBoolDefault(q, "add_data", false),
		AddScopeRoles:  parseBoolDefault(q, "add_scope_roles", false),
		// never expose the stored password hash to the management UI; an
		// absent password on update means "keep current" (see PutUser).
		Sanitize: true,
	}

	user, err := m.cache.GetUser(req)
	if err != nil {
		handleIAMError(w, err, "cannot get user")
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.UserExtended]{Payload: user})
}

func (m *Auth) CreateUserAPI(w http.ResponseWriter, r *http.Request) {
	var userCreate data.UserCreate
	if err := httputil.Decode(r, &userCreate); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode user", err, http.StatusBadRequest))
		return
	}

	user := userCreate.User
	user.ServiceAccount = isServiceAccount(r)
	user.Disabled = !userCreate.IsActive

	id, err := m.store.CreateUser(userCtx(r), user)
	if err != nil {
		handleIAMError(w, err, "cannot create user")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreate]{Payload: data.ResponseCreate{ID: id}})
}

func (m *Auth) PutUserAPI(w http.ResponseWriter, r *http.Request) {
	var req userPutRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode user", err, http.StatusBadRequest))
		return
	}

	user := req.User
	user.ID = r.PathValue("id")
	user.ServiceAccount = isServiceAccount(r)
	if req.IsActive != nil {
		user.Disabled = !*req.IsActive
	}

	if err := m.store.PutUser(userCtx(r), user); err != nil {
		handleIAMError(w, err, "cannot update user")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("user updated"))
}

func (m *Auth) PatchUserAPI(w http.ResponseWriter, r *http.Request) {
	var patch data.UserPatch
	if err := httputil.Decode(r, &patch); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode user patch", err, http.StatusBadRequest))
		return
	}

	if err := m.store.PatchUser(userCtx(r), r.PathValue("id"), patch); err != nil {
		handleIAMError(w, err, "cannot patch user")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("user updated"))
}

func (m *Auth) AccessUserAPI(w http.ResponseWriter, r *http.Request) {
	var userAccess data.UserAccess
	if err := httputil.Decode(r, &userAccess); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode user access", err, http.StatusBadRequest))
		return
	}

	if err := m.store.PatchUserAccess(userCtx(r), r.PathValue("id"), userAccess); err != nil {
		handleIAMError(w, err, "cannot update user access")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("user access updated"))
}

func (m *Auth) DeleteUserAPI(w http.ResponseWriter, r *http.Request) {
	if err := m.store.DeleteUser(userCtx(r), r.PathValue("id")); err != nil {
		handleIAMError(w, err, "cannot delete user")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("user deleted"))
}

// ////////////////////////////////////////////////////////////////////
// service accounts

func (m *Auth) GetServiceAccountsAPI(w http.ResponseWriter, r *http.Request) {
	m.GetUsersAPI(w, wrapServiceAccount(r))
}

func (m *Auth) ExportServiceAccountsAPI(w http.ResponseWriter, r *http.Request) {
	m.ExportUsersAPI(w, wrapServiceAccount(r))
}

func (m *Auth) GetServiceAccountAPI(w http.ResponseWriter, r *http.Request) {
	m.GetUserAPI(w, wrapServiceAccount(r))
}

func (m *Auth) CreateServiceAccountAPI(w http.ResponseWriter, r *http.Request) {
	m.CreateUserAPI(w, wrapServiceAccount(r))
}

func (m *Auth) PutServiceAccountAPI(w http.ResponseWriter, r *http.Request) {
	m.PutUserAPI(w, wrapServiceAccount(r))
}

func (m *Auth) PatchServiceAccountAPI(w http.ResponseWriter, r *http.Request) {
	m.PatchUserAPI(w, wrapServiceAccount(r))
}

func (m *Auth) AccessServiceAccountAPI(w http.ResponseWriter, r *http.Request) {
	m.AccessUserAPI(w, wrapServiceAccount(r))
}

func (m *Auth) DeleteServiceAccountAPI(w http.ResponseWriter, r *http.Request) {
	m.DeleteUserAPI(w, wrapServiceAccount(r))
}

// ////////////////////////////////////////////////////////////////////
// roles

func (m *Auth) GetRolesAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)

	req := data.GetRoleRequest{
		ID:             q.GetValue("id"),
		Name:           q.GetValue("name"),
		Description:    q.GetValue("description"),
		Path:           q.GetValue("path"),
		Method:         q.GetValue("method"),
		PermissionIDs:  q.GetValues("permission_ids"),
		RoleIDs:        q.GetValues("role_ids"),
		Permissions:    q.GetValues("permission"),
		AddRoles:       parseBoolDefault(q, "add_roles", true),
		AddPermissions: parseBoolDefault(q, "add_permissions", false),
		AddTotalUsers:  parseBoolDefault(q, "add_total_users", false),
	}

	req.Limit, req.Offset = getLimitOffset(q)

	roles, err := m.cache.GetRoles(req)
	if err != nil {
		handleIAMError(w, err, "cannot get roles")
		return
	}

	httputil.JSON(w, http.StatusOK, roles)
}

func (m *Auth) ExportRolesAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)
	req := data.GetRoleRequest{
		ID:             q.GetValue("id"),
		Name:           q.GetValue("name"),
		Description:    q.GetValue("description"),
		Path:           q.GetValue("path"),
		Method:         q.GetValue("method"),
		PermissionIDs:  q.GetValues("permission_ids"),
		RoleIDs:        q.GetValues("role_ids"),
		Permissions:    q.GetValues("permission"),
		AddRoles:       true,
		AddPermissions: true,
		AddTotalUsers:  true,
	}

	roles, err := m.cache.GetRoles(req)
	if err != nil {
		handleIAMError(w, err, "cannot export roles")
		return
	}

	headers := []string{"Name", "Permissions", "Roles", "Description", "Total Users"}
	rows := make([]map[string]any, 0, len(roles.Payload))
	for _, role := range roles.Payload {
		permissionNames := make([]string, 0, len(role.Permissions))
		for _, permission := range role.Permissions {
			permissionNames = append(permissionNames, permission.Name)
		}

		roleNames := make([]string, 0, len(role.Roles))
		for _, relatedRole := range role.Roles {
			roleNames = append(roleNames, relatedRole.Name)
		}

		permissionsRaw, _ := json.Marshal(permissionNames)
		rolesRaw, _ := json.Marshal(roleNames)
		rows = append(rows, map[string]any{
			"Name":        role.Name,
			"Permissions": string(permissionsRaw),
			"Roles":       string(rolesRaw),
			"Description": role.Description,
			"Total Users": role.TotalUsers,
		})
	}

	fileName := "roles_" + time.Now().Format("20060102T1504") + ".csv"
	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, rows, fileName); err != nil {
		handleIAMError(w, err, "cannot export roles")
	}
}

func (m *Auth) PutRoleRelationAPI(w http.ResponseWriter, r *http.Request) {
	relation := map[string]data.RoleRelation{}
	if err := httputil.Decode(r, &relation); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode role relation", err, http.StatusBadRequest))
		return
	}

	if err := m.store.PutRoleRelation(userCtx(r), relation); err != nil {
		handleIAMError(w, err, "cannot save role relation")
		return
	}

	m.reload(r)
	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("role relation updated"))
}

func (m *Auth) GetRoleRelationAPI(w http.ResponseWriter, r *http.Request) {
	relation, err := m.store.GetRoleRelation(r.Context())
	if err != nil {
		handleIAMError(w, err, "cannot get role relation")
		return
	}

	httputil.JSON(w, http.StatusOK, relation)
}

func (m *Auth) GetRoleAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)

	role, err := m.cache.GetRole(data.GetRoleRequest{
		ID:             r.PathValue("id"),
		AddRoles:       parseBoolDefault(q, "add_roles", true),
		AddPermissions: parseBoolDefault(q, "add_permissions", false),
		AddTotalUsers:  parseBoolDefault(q, "add_total_users", false),
	})
	if err != nil {
		handleIAMError(w, err, "cannot get role")
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.RoleExtended]{Payload: role})
}

func (m *Auth) CreateRoleAPI(w http.ResponseWriter, r *http.Request) {
	var role data.Role
	if err := httputil.Decode(r, &role); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode role", err, http.StatusBadRequest))
		return
	}

	id, err := m.store.CreateRole(userCtx(r), role)
	if err != nil {
		handleIAMError(w, err, "cannot create role")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreate]{Payload: data.ResponseCreate{ID: id}})
}

func (m *Auth) PutRoleAPI(w http.ResponseWriter, r *http.Request) {
	var role data.Role
	if err := httputil.Decode(r, &role); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode role", err, http.StatusBadRequest))
		return
	}

	role.ID = r.PathValue("id")

	if err := m.store.PutRole(userCtx(r), role); err != nil {
		handleIAMError(w, err, "cannot update role")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("role updated"))
}

func (m *Auth) PatchRoleAPI(w http.ResponseWriter, r *http.Request) {
	var patch data.RolePatch
	if err := httputil.Decode(r, &patch); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode role patch", err, http.StatusBadRequest))
		return
	}

	if err := m.store.PatchRole(userCtx(r), r.PathValue("id"), patch); err != nil {
		handleIAMError(w, err, "cannot patch role")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("role updated"))
}

func (m *Auth) DeleteRoleAPI(w http.ResponseWriter, r *http.Request) {
	if err := m.store.DeleteRole(userCtx(r), r.PathValue("id")); err != nil {
		handleIAMError(w, err, "cannot delete role")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("role deleted"))
}

// ////////////////////////////////////////////////////////////////////
// permissions

func (m *Auth) GetPermissionsAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)

	dataFilter := map[string]string{}
	if v := q.GetValue("data"); v != "" {
		dataFilter["value"] = v
	}

	req := data.GetPermissionRequest{
		ID:          q.GetValue("id"),
		Name:        q.GetValue("name"),
		Description: q.GetValue("description"),
		Path:        q.GetValue("path"),
		Method:      q.GetValue("method"),
		Data:        dataFilter,
		AddRoles:    parseBoolDefault(q, "add_roles", false),
	}

	req.Limit, req.Offset = getLimitOffset(q)

	permissions, err := m.cache.GetPermissions(req)
	if err != nil {
		handleIAMError(w, err, "cannot get permissions")
		return
	}

	httputil.JSON(w, http.StatusOK, permissions)
}

func (m *Auth) ExportPermissionsAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)
	req := data.GetPermissionRequest{
		ID:          q.GetValue("id"),
		Name:        q.GetValue("name"),
		Description: q.GetValue("description"),
		Path:        q.GetValue("path"),
		Method:      q.GetValue("method"),
	}

	permissions, err := m.cache.GetPermissions(req)
	if err != nil {
		handleIAMError(w, err, "cannot export permissions")
		return
	}

	headers := []string{"Name", "Description", "Resources"}
	rows := make([]map[string]any, 0, len(permissions.Payload))
	for _, permission := range permissions.Payload {
		resourcesRaw, _ := json.Marshal(permission.Resources)
		rows = append(rows, map[string]any{
			"Name":        permission.Name,
			"Description": permission.Description,
			"Resources":   string(resourcesRaw),
		})
	}

	fileName := "permissions_" + time.Now().Format("20060102T1504") + ".csv"
	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, rows, fileName); err != nil {
		handleIAMError(w, err, "cannot export permissions")
	}
}

func (m *Auth) GetPermissionAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)

	permission, err := m.cache.GetPermission(data.GetPermissionRequest{
		ID:       r.PathValue("id"),
		AddRoles: parseBoolDefault(q, "add_roles", false),
	})
	if err != nil {
		handleIAMError(w, err, "cannot get permission")
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.PermissionExtended]{Payload: permission})
}

func (m *Auth) CreatePermissionAPI(w http.ResponseWriter, r *http.Request) {
	var permission data.Permission
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode permission", err, http.StatusBadRequest))
		return
	}

	id, err := m.store.CreatePermission(userCtx(r), permission)
	if err != nil {
		handleIAMError(w, err, "cannot create permission")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreate]{Payload: data.ResponseCreate{ID: id}})
}

func (m *Auth) CreatePermissionBulkAPI(w http.ResponseWriter, r *http.Request) {
	var permissions []data.Permission
	if err := httputil.Decode(r, &permissions); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode permissions", err, http.StatusBadRequest))
		return
	}

	ids, err := m.store.CreatePermissions(userCtx(r), permissions)
	if err != nil {
		handleIAMError(w, err, "cannot create permissions")
		return
	}

	m.reload(r)
	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreateBulk]{Payload: data.ResponseCreateBulk{IDs: ids}})
}

func (m *Auth) KeepPermissionBulkAPI(w http.ResponseWriter, r *http.Request) {
	var permissions []data.NameRequest
	if err := httputil.Decode(r, &permissions); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode permissions", err, http.StatusBadRequest))
		return
	}

	keep := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		keep[permission.Name] = struct{}{}
	}

	deleted, err := m.store.KeepPermissions(userCtx(r), keep)
	if err != nil {
		handleIAMError(w, err, "cannot keep permissions")
		return
	}

	m.reload(r)
	httputil.JSON(w, http.StatusOK, data.Response[[]data.IDName]{Payload: deleted})
}

func (m *Auth) PutPermissionAPI(w http.ResponseWriter, r *http.Request) {
	var permission data.Permission
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode permission", err, http.StatusBadRequest))
		return
	}

	permission.ID = r.PathValue("id")

	if err := m.store.PutPermission(userCtx(r), permission); err != nil {
		handleIAMError(w, err, "cannot update permission")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("permission updated"))
}

func (m *Auth) PatchPermissionAPI(w http.ResponseWriter, r *http.Request) {
	var patch data.PermissionPatch
	if err := httputil.Decode(r, &patch); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode permission patch", err, http.StatusBadRequest))
		return
	}

	if err := m.store.PatchPermission(userCtx(r), r.PathValue("id"), patch); err != nil {
		handleIAMError(w, err, "cannot patch permission")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("permission updated"))
}

func (m *Auth) DeletePermissionAPI(w http.ResponseWriter, r *http.Request) {
	if err := m.store.DeletePermission(userCtx(r), r.PathValue("id")); err != nil {
		handleIAMError(w, err, "cannot delete permission")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("permission deleted"))
}

// ////////////////////////////////////////////////////////////////////
// lmaps

func (m *Auth) GetLMapsAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)

	req := data.GetLMapRequest{
		Name:    q.GetValue("name"),
		RoleIDs: q.GetValues("role_ids"),
	}

	req.Limit, req.Offset = getLimitOffset(q)

	lmaps, err := m.cache.GetLMaps(req)
	if err != nil {
		handleIAMError(w, err, "cannot get lmaps")
		return
	}

	httputil.JSON(w, http.StatusOK, lmaps)
}

func (m *Auth) GetLMapAPI(w http.ResponseWriter, r *http.Request) {
	lmap, err := m.cache.GetLMap(r.PathValue("name"))
	if err != nil {
		handleIAMError(w, err, "cannot get lmap")
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.LMap]{Payload: lmap})
}

func (m *Auth) CreateLMapAPI(w http.ResponseWriter, r *http.Request) {
	var lmap data.LMap
	if err := httputil.Decode(r, &lmap); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode lmap", err, http.StatusBadRequest))
		return
	}

	if err := m.store.CreateLMap(userCtx(r), lmap); err != nil {
		handleIAMError(w, err, "cannot create lmap")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("lmap created"))
}

func (m *Auth) PutLMapAPI(w http.ResponseWriter, r *http.Request) {
	var lmap data.LMap
	if err := httputil.Decode(r, &lmap); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode lmap", err, http.StatusBadRequest))
		return
	}

	lmap.Name = r.PathValue("name")

	if err := m.store.PutLMap(userCtx(r), lmap); err != nil {
		handleIAMError(w, err, "cannot update lmap")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("lmap updated"))
}

func (m *Auth) DeleteLMapAPI(w http.ResponseWriter, r *http.Request) {
	if err := m.store.DeleteLMap(userCtx(r), r.PathValue("name")); err != nil {
		handleIAMError(w, err, "cannot delete lmap")
		return
	}

	m.reload(r)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("lmap deleted"))
}

// ////////////////////////////////////////////////////////////////////
// check + dashboard

func (m *Auth) CheckAPI(w http.ResponseWriter, r *http.Request) {
	var body data.CheckRequest
	if err := httputil.Decode(r, &body); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode check request", err, http.StatusBadRequest))
		return
	}

	if body.Alias == "" && body.ID == "" {
		httputil.HandleError(w, httputil.NewError("alias or id is required", nil, http.StatusBadRequest))
		return
	}

	resp, err := m.checkAccess(r.Context(), body)
	if err != nil {
		handleIAMError(w, err, "cannot check access")
		return
	}

	httputil.JSON(w, http.StatusOK, resp)
}

func (m *Auth) checkAccess(ctx context.Context, req data.CheckRequest) (*data.CheckResponse, error) {
	resp, err := m.cache.Check(req)
	if err != nil || resp.Allowed {
		return resp, err
	}

	principal := req.ID
	if principal == "" {
		principal = req.Alias
	}
	if !strings.HasPrefix(principal, apiKeyPrincipalPrefix) {
		return resp, nil
	}

	meta, err := m.store.GetAPIKeyPrincipalByID(ctx, principal)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return resp, nil
		}

		return nil, err
	}

	owner, err := m.cache.GetUser(data.GetUserRequest{ID: meta.UserID})
	if err != nil || owner.Disabled {
		return resp, nil
	}

	user := m.apiKeyUser(meta)

	return m.cache.Snapshot().checkUser(user.User, req), nil
}

// CheckUserAPI checks the X-User header identity against the request body.
func (m *Auth) CheckUserAPI(w http.ResponseWriter, r *http.Request) {
	xUser := r.Header.Get("X-User")
	if xUser == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusUnauthorized))
		return
	}

	var body data.CheckRequestUser
	if err := httputil.Decode(r, &body); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode check request", err, http.StatusBadRequest))
		return
	}

	resp, err := m.checkAccess(r.Context(), data.CheckRequest{
		Alias:  xUser,
		Host:   body.Host,
		Path:   body.Path,
		Method: body.Method,
	})
	if err != nil {
		handleIAMError(w, err, "cannot check access")
		return
	}

	httputil.JSON(w, http.StatusOK, resp)
}

// UserInfoAPI returns identity info for the X-User header.
func (m *Auth) UserInfoAPI(w http.ResponseWriter, r *http.Request) {
	xUser := r.Header.Get("X-User")
	if xUser == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusUnauthorized))
		return
	}

	getData := parseBoolDefault(parseListQuery(r), "data", false)

	user, err := m.cache.GetUser(data.GetUserRequest{
		Alias:          xUser,
		AddRoles:       true,
		AddPermissions: true,
		AddData:        getData,
		Sanitize:       true,
	})
	if err != nil {
		handleIAMError(w, err, "cannot get user info")
		return
	}

	roles := make([]string, 0, len(user.Roles))
	for _, role := range user.Roles {
		roles = append(roles, role.Name)
	}

	permissions := make([]string, 0, len(user.Permissions))
	for _, permission := range user.Permissions {
		permissions = append(permissions, permission.Name)
	}

	httputil.JSON(w, http.StatusOK, data.Response[data.UserInfo]{Payload: data.UserInfo{
		Details:     user.Details,
		Roles:       roles,
		Permissions: permissions,
		Data:        user.Data,
		IsActive:    user.IsActive,
	}})
}

func (m *Auth) DashboardAPI(w http.ResponseWriter, r *http.Request) {
	dashboard, err := m.cache.Dashboard()
	if err != nil {
		handleIAMError(w, err, "cannot get dashboard")
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.Dashboard]{Payload: dashboard})
}

func (m *Auth) VersionAPI(w http.ResponseWriter, r *http.Request) {
	version, err := m.store.Version(r.Context())
	if err != nil {
		handleIAMError(w, err, "cannot get version")
		return
	}

	httputil.JSON(w, http.StatusOK, data.ResponseVersion{Version: version})
}

func (m *Auth) SyncAPI(w http.ResponseWriter, r *http.Request) {
	if err := m.cache.Reload(r.Context()); err != nil {
		handleIAMError(w, err, "cannot sync")
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("synced"))
}
