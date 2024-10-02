package rebac

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
)

// @Title ReBAC API
// @BasePath /
// @description Authorization server with Relationship Based Access Control (ReBAC) model.
//
//go:generate swag init -pd -d ./ -g api.go --ot json -o ./files
func (m *Rebac) MuxSet(prefix string) *chi.Mux {
	mux := chi.NewMux()

	prefix = strings.TrimRight(prefix, "/")

	mux.Get(prefix+"/v1/users", m.GetUsers)
	mux.Post(prefix+"/v1/users", m.CreateUser)
	mux.Get(prefix+"/v1/users/export", m.ExportUsers)
	mux.Get(prefix+"/v1/users/{id}", m.GetUser)
	mux.Patch(prefix+"/v1/users/{id}", m.PatchUser)
	mux.Put(prefix+"/v1/users/{id}", m.PutUser)
	mux.Delete(prefix+"/v1/users/{id}", m.DeleteUser)

	mux.Get(prefix+"/v1/service-accounts", m.GetServiceAccounts)
	mux.Post(prefix+"/v1/service-accounts", m.CreateServiceAccount)
	mux.Get(prefix+"/v1/service-accounts/export", m.ExportServiceAccounts)
	mux.Get(prefix+"/v1/service-accounts/{id}", m.GetServiceAccount)
	mux.Patch(prefix+"/v1/service-accounts/{id}", m.PatchServiceAccount)
	mux.Put(prefix+"/v1/service-accounts/{id}", m.PutServiceAccount)
	mux.Delete(prefix+"/v1/service-accounts/{id}", m.DeleteServiceAccount)

	mux.Get(prefix+"/v1/roles", m.GetRoles)
	mux.Post(prefix+"/v1/roles", m.CreateRole)
	mux.Get(prefix+"/v1/roles/export", m.ExportRoles)
	mux.Get(prefix+"/v1/roles/{id}", m.GetRole)
	mux.Patch(prefix+"/v1/roles/{id}", m.PatchRole)
	mux.Put(prefix+"/v1/roles/{id}", m.PutRole)
	mux.Delete(prefix+"/v1/roles/{id}", m.DeleteRole)

	mux.Get(prefix+"/v1/permissions", m.GetPermissions)
	mux.Post(prefix+"/v1/permissions", m.CreatePermission)
	mux.Get(prefix+"/v1/permissions/export", m.ExportPermissions)
	mux.Get(prefix+"/v1/permissions/{id}", m.GetPermission)
	mux.Patch(prefix+"/v1/permissions/{id}", m.PatchPermission)
	mux.Put(prefix+"/v1/permissions/{id}", m.PutPermission)
	mux.Delete(prefix+"/v1/permissions/{id}", m.DeletePermission)

	mux.Get(prefix+"/v1/ldap/users/{uid}", m.LdapGetUsers)
	mux.Get(prefix+"/v1/ldap/groups", m.LdapGetGroups)
	mux.Post(prefix+"/v1/ldap/sync", m.LdapSyncGroups)
	mux.Post(prefix+"/v1/ldap/sync/{uid}", m.LdapSyncGroupsUID)

	mux.Get(prefix+"/v1/ldap/maps", m.LdapGetGroupMaps)
	mux.Post(prefix+"/v1/ldap/maps", m.LdapCreateGroupMaps)
	mux.Get(prefix+"/v1/ldap/maps/{name}", m.LdapGetGroupMap)
	mux.Put(prefix+"/v1/ldap/maps/{name}", m.LdapPutGroupMaps)
	mux.Delete(prefix+"/v1/ldap/maps/{name}", m.LdapDeleteGroupMaps)

	mux.Post(prefix+"/v1/check", m.PostCheck)
	mux.Get(prefix+"/v1/info", m.Info)

	mux.Post(prefix+"/check", m.PostCheckUser)
	mux.Get(prefix+"/info", m.InfoUser)

	mux.Get(prefix+"/v1/backup", m.Backup)
	mux.Post(prefix+"/v1/restore", m.Restore)

	mux.Get(prefix+"/ui/info", m.UIInfo)
	mux.Handle(prefix+"/swagger/*", m.swaggerFS)
	mux.Handle(prefix+"/ui/*", m.uiFS)

	return mux
}

func getLimitOffset(v url.Values) (limit, offset int64) {
	limit, _ = strconv.ParseInt(v.Get("limit"), 10, 64)
	offset, _ = strconv.ParseInt(v.Get("offset"), 10, 64)

	return
}

func (m *Rebac) UIInfo(w http.ResponseWriter, _ *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]interface{}{
		"prefix_path": m.PrefixPath,
	})
}

func wrapServiceAccount(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), "service_account", true))
}

func isServiceAccount(r *http.Request) bool {
	if v, _ := r.Context().Value("service_account").(bool); v {
		return true
	}

	return false
}

// @Summary Create service account
// @Tags service-accounts
// @Param user body data.User true "Service Account"
// @Success 200 {object} data.Response[data.ResponseCreate]
// @Failure 400 {object} data.ResponseError
// @Failure 409 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts [POST]
func (m *Rebac) CreateServiceAccount(w http.ResponseWriter, r *http.Request) {
	m.CreateUser(w, wrapServiceAccount(r))
}

// @Summary Get service accounts
// @Tags service-accounts
// @Param alias query string false "service alias"
// @Param id query string false "service id"
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param uid query string false "details->uid"
// @Param name query string false "details->name"
// @Param email query string false "details->email"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Param disabled query bool false "disabled"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_permissions query bool false "add permissions"
// @Param add_datas query bool false "add datas"
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.UserExtended]
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts [GET]
func (m *Rebac) GetServiceAccounts(w http.ResponseWriter, r *http.Request) {
	m.GetUsers(w, wrapServiceAccount(r))
}

// @Summary Export service accounts
// @Tags service-accounts
// @Param alias query string false "service alias"
// @Param id query string false "service id"
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param uid query string false "details->uid"
// @Param name query string false "details->name"
// @Param email query string false "details->email"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Param disabled query bool false "disabled"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts/export [GET]
func (m *Rebac) ExportServiceAccounts(w http.ResponseWriter, r *http.Request) {
	m.ExportUsers(w, wrapServiceAccount(r))
}

// @Summary Get service account
// @Tags service-accounts
// @Param id path string true "service id"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_permissions query bool false "add permissions"
// @Param add_datas query bool false "add datas"
// @Success 200 {object} data.Response[data.UserExtended]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts/{id} [GET]
func (m *Rebac) GetServiceAccount(w http.ResponseWriter, r *http.Request) {
	m.GetUser(w, wrapServiceAccount(r))
}

// @Summary Patch service account
// @Tags service-accounts
// @Param id path string true "service id"
// @Param user body data.UserPatch true "Service"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts/{id} [PATCH]
func (m *Rebac) PatchServiceAccount(w http.ResponseWriter, r *http.Request) {
	m.PatchUser(w, wrapServiceAccount(r))
}

// @Summary Put service account
// @Tags service-accounts
// @Param id path string true "service id"
// @Param user body data.User true "Service"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts/{id} [PUT]
func (m *Rebac) PutServiceAccount(w http.ResponseWriter, r *http.Request) {
	m.PutUser(w, wrapServiceAccount(r))
}

// @Summary Delete service account
// @Tags service-accounts
// @Param id path string true "service id"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts/{id} [DELETE]
func (m *Rebac) DeleteServiceAccount(w http.ResponseWriter, r *http.Request) {
	m.DeleteUser(w, wrapServiceAccount(r))
}

// @Summary Create user
// @Tags users
// @Param user body data.User true "User"
// @Success 200 {object} data.Response[data.ResponseCreate]
// @Failure 400 {object} data.ResponseError
// @Failure 409 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/users [POST]
func (m *Rebac) CreateUser(w http.ResponseWriter, r *http.Request) {
	user := data.User{}
	if err := httputil.Decode(r, &user); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	user.ServiceAccount = isServiceAccount(r)

	id, err := m.db.CreateUser(user)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, data.NewError("User already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot create user", err, http.StatusInternalServerError))
		return
	}

	text := "User created"
	if user.ServiceAccount {
		text = "Service account created"
	}

	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreate]{
		Message: &data.Message{
			Text: text,
		},
		Payload: data.ResponseCreate{
			ID: id,
		},
	})
}

// @Summary Get users
// @Tags users
// @Param alias query string false "user alias"
// @Param id query string false "user id"
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param uid query string false "details->uid"
// @Param name query string false "details->name"
// @Param email query string false "details->email"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Param disabled query bool false "disabled"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_permissions query bool false "add permissions"
// @Param add_datas query bool false "add datas"
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.UserExtended]
// @Failure 500 {object} data.ResponseError
// @Router /v1/users [GET]
func (m *Rebac) GetUsers(w http.ResponseWriter, r *http.Request) {
	req := data.GetUserRequest{
		AddRoles:       true,
		ServiceAccount: isServiceAccount(r),
	}

	query := r.URL.Query()
	req.Alias = query.Get("alias")
	req.ID = query.Get("id")

	req.RoleIDs = httputil.CommaQueryParam(query["role_ids"])

	req.UID = query.Get("uid")
	req.Name = query.Get("name")
	req.Email = query.Get("email")

	req.Path = query.Get("path")
	req.Method = query.Get("method")

	req.Disabled, _ = strconv.ParseBool(query.Get("disabled"))

	if addRoles, err := strconv.ParseBool(r.URL.Query().Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}
	req.AddPermissions, _ = strconv.ParseBool(r.URL.Query().Get("add_permissions"))
	req.AddDatas, _ = strconv.ParseBool(r.URL.Query().Get("add_datas"))

	req.Limit, req.Offset = getLimitOffset(query)

	users, err := m.db.GetUsers(req)
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot get users", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, users)
}

// @Summary Export users
// @Tags users
// @Param alias query string false "user alias"
// @Param id query string false "user id"
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param uid query string false "details->uid"
// @Param name query string false "details->name"
// @Param email query string false "details->email"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Param disabled query bool false "disabled"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/users/export [GET]
func (m *Rebac) ExportUsers(w http.ResponseWriter, r *http.Request) {
	req := data.GetUserRequest{
		AddRoles:       true,
		ServiceAccount: isServiceAccount(r),
	}

	query := r.URL.Query()
	req.Alias = query.Get("alias")
	req.ID = query.Get("id")

	req.RoleIDs = httputil.CommaQueryParam(query["role_ids"])

	req.UID = query.Get("uid")
	req.Name = query.Get("name")
	req.Email = query.Get("email")

	req.Path = query.Get("path")
	req.Method = query.Get("method")

	req.Disabled, _ = strconv.ParseBool(query.Get("disabled"))

	users, err := m.db.GetUsers(req)
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot get users", err, http.StatusInternalServerError))
		return
	}

	// download the result as CSV
	headers := []string{"UID", "Name", "Email", "Roles", "Disabled"}

	userDatas := make([]map[string]interface{}, 0, len(users.Payload))
	for _, user := range users.Payload {
		if user.Details == nil {
			user.Details = map[string]interface{}{}
		}

		roles := make([]string, 0, len(user.Roles))
		for _, role := range user.Roles {
			roles = append(roles, role.Name)
		}

		userDatas = append(userDatas, map[string]interface{}{
			"UID":      user.Details["uid"],
			"Name":     user.Details["name"],
			"Email":    user.Details["email"],
			"Roles":    strings.Join(roles, ","),
			"Disabled": user.Disabled,
		})
	}

	fileName := time.Now().Format("20060102T1504") + ".csv"
	if req.ServiceAccount {
		fileName = "service_" + fileName
	} else {
		fileName = "user_" + fileName
	}

	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, userDatas, fileName); err != nil {
		httputil.HandleError(w, data.NewError("Cannot export users", err, http.StatusInternalServerError))
		return
	}
}

// @Summary Get user
// @Tags users
// @Param id path string true "user id"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_permissions query bool false "add permissions"
// @Param add_datas query bool false "add datas"
// @Success 200 {object} data.Response[data.UserExtended]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/users/{id} [GET]
func (m *Rebac) GetUser(w http.ResponseWriter, r *http.Request) {
	req := data.GetUserRequest{
		AddRoles:       true,
		ServiceAccount: isServiceAccount(r),
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	req.ID = id

	if addRoles, err := strconv.ParseBool(r.URL.Query().Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}
	req.AddPermissions, _ = strconv.ParseBool(r.URL.Query().Get("add_permissions"))
	req.AddDatas, _ = strconv.ParseBool(r.URL.Query().Get("add_datas"))

	user, err := m.db.GetUser(req)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot get user", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.UserExtended]{
		Payload: user,
	})
}

// @Summary Delete user
// @Tags users
// @Param id path string true "user id"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/users/{id} [DELETE]
func (m *Rebac) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.db.DeleteUser(id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot delete user", err, http.StatusInternalServerError))
		return
	}

	if isServiceAccount(r) {
		httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Service account deleted"))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("User deleted"))
}

// @Summary Patch user
// @Tags users
// @Param id path string true "user id"
// @Param user body data.UserPatch true "User"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/users/{id} [PATCH]
func (m *Rebac) PatchUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	userPatch := data.UserPatch{}
	if err := httputil.Decode(r, &userPatch); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	if err := m.db.PatchUser(id, userPatch); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot patch user", err, http.StatusInternalServerError))
		return
	}

	if isServiceAccount(r) {
		httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Service account's data patched"))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("User's data patched"))
}

// @Summary Put user
// @Tags users
// @Param id path string true "user id"
// @Param user body data.User true "User"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/users/{id} [PUT]
func (m *Rebac) PutUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	user := data.User{}
	if err := httputil.Decode(r, &user); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	user.ID = id
	user.ServiceAccount = isServiceAccount(r)

	if err := m.db.PutUser(user); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot put user", err, http.StatusInternalServerError))
		return
	}

	if user.ServiceAccount {
		httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Service account's data replaced"))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("User's data replaced"))
}

// @Summary Get roles
// @Tags roles
// @Param name query string false "role name"
// @Param id query string false "role id"
// @Param permission_ids query []string false "role permission ids" collectionFormat(multi)
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param method query string false "request method"
// @Param path query string false "request path"
// @Param add_permissions query bool false "add permissions default(true)"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_total_users query bool false "add total users default(true)"
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.RoleExtended]
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles [GET]
func (m *Rebac) GetRoles(w http.ResponseWriter, r *http.Request) {
	req := data.GetRoleRequest{
		AddPermissions: true,
		AddRoles:       true,
		AddTotalUsers:  true,
	}

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.PermissionIDs = httputil.CommaQueryParam(query["permission_ids"])
	req.RoleIDs = httputil.CommaQueryParam(query["role_ids"])
	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Limit, req.Offset = getLimitOffset(query)

	if addPermissions, err := strconv.ParseBool(r.URL.Query().Get("add_permissions")); err == nil {
		req.AddPermissions = addPermissions
	}

	if addRoles, err := strconv.ParseBool(r.URL.Query().Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}

	if addTotalUsers, err := strconv.ParseBool(r.URL.Query().Get("add_total_users")); err == nil {
		req.AddTotalUsers = addTotalUsers
	}

	roles, err := m.db.GetRoles(req)
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot get roles", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, roles)
}

// @Summary Export roles
// @Tags roles
// @Param name query string false "role name"
// @Param id query string false "role id"
// @Param permission_ids query []string false "role permission ids" collectionFormat(multi)
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param method query string false "request method"
// @Param path query string false "request path"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles/export [GET]
func (m *Rebac) ExportRoles(w http.ResponseWriter, r *http.Request) {
	req := data.GetRoleRequest{
		AddPermissions: true,
		AddRoles:       true,
		AddTotalUsers:  true,
	}

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.PermissionIDs = httputil.CommaQueryParam(query["permission_ids"])
	req.RoleIDs = httputil.CommaQueryParam(query["role_ids"])
	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Limit, req.Offset = getLimitOffset(query)

	if addPermissions, err := strconv.ParseBool(r.URL.Query().Get("add_permissions")); err == nil {
		req.AddPermissions = addPermissions
	}

	if addRoles, err := strconv.ParseBool(r.URL.Query().Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}

	if addTotalUsers, err := strconv.ParseBool(r.URL.Query().Get("add_total_users")); err == nil {
		req.AddTotalUsers = addTotalUsers
	}

	roles, err := m.db.GetRoles(req)
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot get roles", err, http.StatusInternalServerError))
		return
	}

	// download the result as CSV
	headers := []string{"Name", "Permissions", "Roles", "Description", "Total Users"}

	roleDatas := make([]map[string]interface{}, 0, len(roles.Payload))
	for _, role := range roles.Payload {
		permissions := make([]string, 0, len(role.Permissions))
		for _, permission := range role.Permissions {
			permissions = append(permissions, permission.Name)
		}

		permissionByte, err := json.Marshal(permissions)
		if err != nil {
			httputil.HandleError(w, data.NewError("Cannot marshal permissions", err, http.StatusInternalServerError))
			return
		}

		roles := make([]string, 0, len(role.Roles))
		for _, r := range role.Roles {
			roles = append(roles, r.Name)
		}

		rolesByte, err := json.Marshal(roles)
		if err != nil {
			httputil.HandleError(w, data.NewError("Cannot marshal roles", err, http.StatusInternalServerError))
			return
		}

		roleDatas = append(roleDatas, map[string]interface{}{
			"Name":        role.Name,
			"Permissions": string(permissionByte),
			"Roles":       string(rolesByte),
			"Description": role.Description,
			"Total Users": humanize.Comma(int64(role.TotalUsers)),
		})
	}

	fileName := "roles_" + time.Now().Format("20060102T1504") + ".csv"

	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, roleDatas, fileName); err != nil {
		httputil.HandleError(w, data.NewError("Cannot export roles", err, http.StatusInternalServerError))
		return
	}
}

// @Summary Create role
// @Tags roles
// @Param role body data.Role true "Role"
// @Success 200 {object} data.Response[data.ResponseCreate]
// @Failure 400 {object} data.ResponseError
// @Failure 409 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles [POST]
func (m *Rebac) CreateRole(w http.ResponseWriter, r *http.Request) {
	role := data.Role{}
	if err := httputil.Decode(r, &role); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	id, err := m.db.CreateRole(role)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, data.NewError("Role already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot create role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreate]{
		Message: &data.Message{
			Text: "Role created",
		},
		Payload: data.ResponseCreate{
			ID: id,
		},
	})
}

// @Summary Get role
// @Tags roles
// @Param id path string true "role ID"
// @Param add_permissions query bool false "add permissions default(true)"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_total_users query bool false "add total users default(true)"
// @Success 200 {object} data.Response[data.RoleExtended]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles/{id} [GET]
func (m *Rebac) GetRole(w http.ResponseWriter, r *http.Request) {
	req := data.GetRoleRequest{
		AddPermissions: true,
		AddRoles:       true,
		AddTotalUsers:  true,
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	req.ID = id

	role, err := m.db.GetRole(req)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot get role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.RoleExtended]{
		Payload: role,
	})
}

// @Summary Patch role
// @Tags roles
// @Param id path string true "role ID"
// @Param role body data.RolePatch true "Role"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles/{id} [PATCH]
func (m *Rebac) PatchRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	rolePatch := data.RolePatch{}
	if err := httputil.Decode(r, &rolePatch); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	if err := m.db.PatchRole(id, rolePatch); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot patch role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.ResponseMessage{
		Message: &data.Message{
			Text: "Role's data patched",
		},
	})
}

// @Summary Put role
// @Tags roles
// @Param id path string true "role ID"
// @Param role body data.Role true "Role"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles/{id} [PUT]
func (m *Rebac) PutRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	role := data.Role{}
	if err := httputil.Decode(r, &role); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	role.ID = id

	if err := m.db.PutRole(role); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot put role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Role's data replaced"))
}

// @Summary Delete role
// @Tags roles
// @Param id path string true "role name"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles/{id} [DELETE]
func (m *Rebac) DeleteRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.db.DeleteRole(id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot delete role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Role deleted"))
}

// @Summary Get permissions
// @Tags permissions
// @Param id query string false "permission id"
// @Param name query string false "permission name"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.Permission]
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions [GET]
func (m *Rebac) GetPermissions(w http.ResponseWriter, r *http.Request) {
	var req data.GetPermissionRequest

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Limit, req.Offset = getLimitOffset(query)

	permissions, err := m.db.GetPermissions(req)
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot get permissions", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, permissions)
}

// @Summary Export permissions
// @Tags permissions
// @Param id query string false "permission id"
// @Param name query string false "permission name"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/export [GET]
func (m *Rebac) ExportPermissions(w http.ResponseWriter, r *http.Request) {
	var req data.GetPermissionRequest

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.Path = query.Get("path")
	req.Method = query.Get("method")

	permissions, err := m.db.GetPermissions(req)
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot get permissions", err, http.StatusInternalServerError))
		return
	}

	// download the result as CSV
	headers := []string{"Name", "Path", "Method"}

	permissionDatas := make([]map[string]interface{}, 0, len(permissions.Payload))
	for _, permission := range permissions.Payload {
		resourceByte, err := json.Marshal(permission.Resources)
		if err != nil {
			httputil.HandleError(w, data.NewError("Cannot marshal resources", err, http.StatusInternalServerError))
			return
		}

		permissionDatas = append(permissionDatas, map[string]interface{}{
			"Name":        permission.Name,
			"Resources":   string(resourceByte),
			"Description": permission.Description,
		})
	}

	fileName := "permissions_" + time.Now().Format("20060102T1504") + ".csv"

	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, permissionDatas, fileName); err != nil {
		httputil.HandleError(w, data.NewError("Cannot export permissions", err, http.StatusInternalServerError))
		return
	}
}

// @Summary Create permission
// @Tags permissions
// @Param permission body data.Permission true "Permission"
// @Success 200 {object} data.Response[data.ResponseCreate]
// @Failure 400 {object} data.ResponseError
// @Failure 409 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions [POST]
func (m *Rebac) CreatePermission(w http.ResponseWriter, r *http.Request) {
	permission := data.Permission{}
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	id, err := m.db.CreatePermission(permission)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, data.NewError("Permission already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot create permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreate]{
		Message: &data.Message{
			Text: "Permission created",
		},
		Payload: data.ResponseCreate{
			ID: id,
		},
	})
}

// @Summary Get permission
// @Tags permissions
// @Param id path string true "permission name"
// @Success 200 {object} data.Response[data.Permission]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/{id} [GET]
func (m *Rebac) GetPermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	permission, err := m.db.GetPermission(id)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot get permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.Permission]{
		Payload: permission,
	})
}

// @Summary Patch permission
// @Tags permissions
// @Param id path string true "permission ID"
// @Param permission body data.PermissionPatch true "Permission"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/{id} [PATCH]
func (m *Rebac) PatchPermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	permission := data.PermissionPatch{}
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	if err := m.db.PatchPermission(id, permission); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, data.NewError("Permission already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot patch permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Permission's data patched"))
}

// @Summary Put permission
// @Tags permissions
// @Param id path string true "permission ID"
// @Param permission body data.Permission true "Permission"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/{id} [PUT]
func (m *Rebac) PutPermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	permission := data.Permission{}
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	permission.ID = id

	if err := m.db.PutPermission(permission); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot put permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Permission's data replaced"))
}

// @Summary Delete permission
// @Tags permissions
// @Param id path string true "permission name"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/{id} [DELETE]
func (m *Rebac) DeletePermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, data.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.db.DeletePermission(id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot delete permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Permission deleted"))
}

// @Summary Post check
// @Tags check
// @Param check body data.CheckRequest true "Check"
// @Success 200 {object} data.CheckResponse
// @Failure 400 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/check [POST]
func (m *Rebac) PostCheck(w http.ResponseWriter, r *http.Request) {
	body := data.CheckRequest{}
	if err := httputil.Decode(r, &body); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	resp, err := m.db.Check(body)
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot check", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, resp)
}

// @Summary Post check
// @Tags public
// @Param X-User header string false "User alias, X-User is authorized user auto adds"
// @Param check body data.CheckRequestUser true "Check"
// @Success 200 {object} data.CheckResponse
// @Failure 400 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /check [POST]
func (m *Rebac) PostCheckUser(w http.ResponseWriter, r *http.Request) {
	user := r.Header.Get("X-User")
	if user == "" {
		httputil.HandleError(w, data.NewError("X-User header is required", nil, http.StatusBadRequest))
		return
	}

	body := data.CheckRequestUser{}
	if err := httputil.Decode(r, &body); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	resp, err := m.db.Check(data.CheckRequest{
		Alias:  user,
		Path:   body.Path,
		Method: body.Method,
	})
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot check", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, resp)
}

// @Summary Create LDAP Map
// @Tags ldap
// @Param map body data.LMap true "Map"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 409 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/maps [POST]
func (m *Rebac) LdapCreateGroupMaps(w http.ResponseWriter, r *http.Request) {
	lmap := data.LMap{}
	if err := httputil.Decode(r, &lmap); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	if err := m.db.CreateLMap(lmap); err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, data.NewError("Map already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot create ldap map", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Map created"))
}

// @Summary Get LDAP Maps
// @Tags ldap
// @Param name query string false "map name"
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.LMap]
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/maps [GET]
func (m *Rebac) LdapGetGroupMaps(w http.ResponseWriter, r *http.Request) {
	req := data.GetLMapRequest{}

	query := r.URL.Query()
	req.Name = query.Get("name")
	req.RoleIDs = query["role_ids"]
	req.Limit, req.Offset = getLimitOffset(query)

	maps, err := m.db.GetLMaps(req)
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot get ldap maps", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, maps)
}

// @Summary Get LDAP Map
// @Tags ldap
// @Param name path string true "map name"
// @Success 200 {object} data.Response[data.LMap]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/maps/{name} [GET]
func (m *Rebac) LdapGetGroupMap(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.HandleError(w, data.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	maps, err := m.db.GetLMap(name)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Map not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot get ldap map", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.LMap]{Payload: maps})
}

// @Summary Put LDAP Map
// @Tags ldap
// @Param name path string true "map name"
// @Param map body data.LMap true "Map"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/maps/{name} [PUT]
func (m *Rebac) LdapPutGroupMaps(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.HandleError(w, data.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	lmap := data.LMap{}
	if err := httputil.Decode(r, &lmap); err != nil {
		httputil.HandleError(w, data.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	lmap.Name = name

	if err := m.db.PutLMap(lmap); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Map not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot put ldap map", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Map replaced"))
}

// @Summary Delete LDAP Map
// @Tags ldap
// @Param name path string true "map name"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/maps/{name} [DELETE]
func (m *Rebac) LdapDeleteGroupMaps(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.HandleError(w, data.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.db.DeleteLMap(name); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("Map not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot delete ldap map", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Map deleted"))
}

// @Summary Get current user's info
// @Tags info
// @Param alias query string true "User alias"
// @Param datas query bool false "add role datas"
// @Success 200 {object} data.Response[data.UserInfo]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/info [GET]
func (m *Rebac) Info(w http.ResponseWriter, r *http.Request) {
	alias := r.URL.Query().Get("alias")

	if alias == "" {
		httputil.HandleError(w, data.NewError("Alias is required", nil, http.StatusBadRequest))
		return
	}

	getData, _ := strconv.ParseBool(r.URL.Query().Get("datas"))

	user, err := m.db.GetUser(data.GetUserRequest{Alias: alias, AddRoles: true, AddPermissions: true, AddDatas: getData})
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot get user", err, http.StatusInternalServerError))
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

	userInfo := data.UserInfo{
		Details:     user.Details,
		Roles:       roles,
		Permissions: permissions,
		Datas:       user.Datas,
		Disabled:    user.Disabled,
	}

	httputil.JSON(w, http.StatusOK, data.Response[data.UserInfo]{Payload: userInfo})
}

// @Summary Get current user's info
// @Tags public
// @Param X-User header string false "User alias, X-User is authorized user auto adds"
// @Param datas query bool false "add role datas"
// @Success 200 {object} data.Response[data.UserInfo]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /info [GET]
func (m *Rebac) InfoUser(w http.ResponseWriter, r *http.Request) {
	xUser := r.Header.Get("X-User")

	if xUser == "" {
		httputil.HandleError(w, data.NewError("X-User header is required", nil, http.StatusBadRequest))
		return
	}

	getData, _ := strconv.ParseBool(r.URL.Query().Get("datas"))

	user, err := m.db.GetUser(data.GetUserRequest{Alias: xUser, AddRoles: true, AddPermissions: true, AddDatas: getData})
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, data.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, data.NewError("Cannot get user", err, http.StatusInternalServerError))
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

	userInfo := data.UserInfo{
		Details:     user.Details,
		Roles:       roles,
		Permissions: permissions,
		Datas:       user.Datas,
		Disabled:    user.Disabled,
	}

	httputil.JSON(w, http.StatusOK, data.Response[data.UserInfo]{Payload: userInfo})
}

// @Summary Backup Database
// @Tags backup
// @Param since query number false "since txid"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/backup [GET]
func (m *Rebac) Backup(w http.ResponseWriter, r *http.Request) {
	var since uint64
	sinceStr := r.URL.Query().Get("since")
	if sinceStr != "" {
		var err error
		since, err = strconv.ParseUint(sinceStr, 10, 64)
		if err != nil {
			httputil.HandleError(w, data.NewError("Cannot parse since", err, http.StatusBadRequest))
			return
		}
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=rebac_"+time.Now().Format("2006-01-02_15-04-05")+".db")
	if err := m.db.Backup(w, since); err != nil {
		httputil.HandleError(w, data.NewError("Cannot backup", err, http.StatusInternalServerError))
		return
	}
}

// @Summary Restore Database
// @Tags backup
// @Param file formData file true "Backup file"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/restore [POST]
func (m *Rebac) Restore(w http.ResponseWriter, r *http.Request) {
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		httputil.HandleError(w, data.NewError("Cannot get file", err, http.StatusBadRequest))
		return
	}
	if file == nil {
		httputil.HandleError(w, data.NewError("File is required", nil, http.StatusBadRequest))
		return
	}

	defer file.Close()

	slog.Info("restoring database",
		slog.String("file_name", fileHeader.Filename),
		slog.String("file_size", humanize.Bytes(uint64(fileHeader.Size))),
	)

	if err := m.db.Restore(file); err != nil {
		httputil.HandleError(w, data.NewError("Cannot restore", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Database restored"))
}
