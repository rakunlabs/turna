package iam

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/cast"

	"github.com/rakunlabs/logi"
	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
)

// @Title IAM API
// @BasePath /
// @description Identity and Access Management API
//
//go:generate swag init -pd -d ./ -g api.go --ot json -o ./files
func (m *Iam) MuxSet(prefix string) *chi.Mux {
	mux := chi.NewMux()

	mux.Get(prefix+"/v1/users", m.GetUsers)
	mux.Post(prefix+"/v1/users", m.CreateUser) // trigger
	mux.Get(prefix+"/v1/users/export", m.ExportUsers)
	mux.Get(prefix+"/v1/users/{id}", m.GetUser)
	mux.Patch(prefix+"/v1/users/{id}", m.PatchUser)   // trigger
	mux.Put(prefix+"/v1/users/{id}", m.PutUser)       // trigger
	mux.Delete(prefix+"/v1/users/{id}", m.DeleteUser) // trigger

	mux.Get(prefix+"/v1/service-accounts", m.GetServiceAccounts)
	mux.Post(prefix+"/v1/service-accounts", m.CreateServiceAccount) // trigger
	mux.Get(prefix+"/v1/service-accounts/export", m.ExportServiceAccounts)
	mux.Get(prefix+"/v1/service-accounts/{id}", m.GetServiceAccount)
	mux.Patch(prefix+"/v1/service-accounts/{id}", m.PatchServiceAccount)   // trigger
	mux.Put(prefix+"/v1/service-accounts/{id}", m.PutServiceAccount)       // trigger
	mux.Delete(prefix+"/v1/service-accounts/{id}", m.DeleteServiceAccount) // trigger

	mux.Get(prefix+"/v1/roles", m.GetRoles)
	mux.Post(prefix+"/v1/roles", m.CreateRole)              // trigger
	mux.Put(prefix+"/v1/roles/relation", m.PutRoleRelation) // trigger
	mux.Get(prefix+"/v1/roles/relation", m.GetRoleRelation)
	mux.Get(prefix+"/v1/roles/export", m.ExportRoles)
	mux.Get(prefix+"/v1/roles/{id}", m.GetRole)
	mux.Patch(prefix+"/v1/roles/{id}", m.PatchRole)   // trigger
	mux.Put(prefix+"/v1/roles/{id}", m.PutRole)       // trigger
	mux.Delete(prefix+"/v1/roles/{id}", m.DeleteRole) // trigger

	mux.Get(prefix+"/v1/permissions", m.GetPermissions)
	mux.Post(prefix+"/v1/permissions", m.CreatePermission)          // trigger
	mux.Post(prefix+"/v1/permissions/bulk", m.CreatePermissionBulk) // trigger
	mux.Get(prefix+"/v1/permissions/export", m.ExportPermissions)
	mux.Post(prefix+"/v1/permissions/keep", m.KeepPermissionBulk) // trigger
	mux.Get(prefix+"/v1/permissions/{id}", m.GetPermission)
	mux.Patch(prefix+"/v1/permissions/{id}", m.PatchPermission)   // trigger
	mux.Put(prefix+"/v1/permissions/{id}", m.PutPermission)       // trigger
	mux.Delete(prefix+"/v1/permissions/{id}", m.DeletePermission) // trigger

	// mux.Get(prefix+"/v1/tokens", m.GetTokens)
	// mux.Post(prefix+"/v1/tokens", m.CreateToken) // trigger
	// mux.Get(prefix+"/v1/tokens/{id}", m.GetToken)
	// mux.Patch(prefix+"/v1/tokens/{id}", m.PatchToken)   // trigger
	// mux.Put(prefix+"/v1/tokens/{id}", m.PutToken)       // trigger
	// mux.Delete(prefix+"/v1/tokens/{id}", m.DeleteToken) // trigger

	mux.Get(prefix+"/v1/ldap/users/{uid}", m.LdapGetUsers)      // trigger
	mux.Get(prefix+"/v1/ldap/groups", m.LdapGetGroups)          // trigger
	mux.Post(prefix+"/v1/ldap/sync", m.LdapSyncGroups)          // trigger
	mux.Post(prefix+"/v1/ldap/sync/{uid}", m.LdapSyncGroupsUID) // trigger

	mux.Get(prefix+"/v1/ldap/maps", m.LdapGetGroupMaps)              // trigger
	mux.Post(prefix+"/v1/ldap/maps", m.LdapCreateGroupMaps)          // trigger
	mux.Get(prefix+"/v1/ldap/maps/{name}", m.LdapGetGroupMap)        // trigger
	mux.Put(prefix+"/v1/ldap/maps/{name}", m.LdapPutGroupMaps)       // trigger
	mux.Delete(prefix+"/v1/ldap/maps/{name}", m.LdapDeleteGroupMaps) // trigger

	mux.Get(prefix+"/v1/dashboard", m.Dashboard)

	mux.Post(prefix+"/v1/check", m.PostCheck)
	mux.Get(prefix+"/v1/info", m.Info)

	mux.Post(prefix+"/check", m.PostCheckUser)
	mux.Get(prefix+"/info", m.InfoUser)

	mux.Get(prefix+"/v1/backup", m.Backup)
	mux.Post(prefix+"/v1/restore", m.Restore) // trigger
	mux.Get(prefix+"/v1/version", m.Version)
	mux.Post(prefix+"/v1/sync", m.Sync)

	mux.Get(prefix+"/ui/info", m.UIInfo)
	mux.Handle(prefix+"/swagger/*", m.swaggerFS)
	mux.Handle(prefix+"/ui/*", m.uiFS)

	return mux
}

func getUserName(r *http.Request) string {
	v := r.Header.Get("X-User")
	if v == "" {
		v = "unknown"
	}

	return v
}

func getLimitOffset(v url.Values) (limit, offset int64) {
	limit, _ = strconv.ParseInt(v.Get("limit"), 10, 64)
	offset, _ = strconv.ParseInt(v.Get("offset"), 10, 64)

	return
}

func (m *Iam) UIInfo(w http.ResponseWriter, _ *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]interface{}{
		"prefix_path": m.PrefixPath,
	})
}

func wrapServiceAccount(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), "service_account", true))
}

func isServiceAccount(r *http.Request) bool {
	v, _ := r.Context().Value("service_account").(bool)

	return v
}

func isServiceAccountPtr(r *http.Request) *bool {
	v := isServiceAccount(r)

	return &v
}

// @Summary Create service account
// @Tags service-accounts
// @Param user body data.UserCreate true "Service Account"
// @Success 200 {object} data.Response[data.ResponseCreate]
// @Failure 400 {object} data.ResponseError
// @Failure 409 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts [POST]
func (m *Iam) CreateServiceAccount(w http.ResponseWriter, r *http.Request) {
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
// @Param permission query []string false "permission" collectionFormat(multi)
// @Param is_active query bool false "is_active"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_permissions query bool false "add permissions"
// @Param add_data query bool false "add data"
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.UserExtended]
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts [GET]
func (m *Iam) GetServiceAccounts(w http.ResponseWriter, r *http.Request) {
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
// @Param is_active query bool false "is_active"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts/export [GET]
func (m *Iam) ExportServiceAccounts(w http.ResponseWriter, r *http.Request) {
	m.ExportUsers(w, wrapServiceAccount(r))
}

// @Summary Get service account
// @Tags service-accounts
// @Param id path string true "service id"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_permissions query bool false "add permissions"
// @Param add_data query bool false "add data"
// @Success 200 {object} data.Response[data.UserExtended]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/service-accounts/{id} [GET]
func (m *Iam) GetServiceAccount(w http.ResponseWriter, r *http.Request) {
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
func (m *Iam) PatchServiceAccount(w http.ResponseWriter, r *http.Request) {
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
func (m *Iam) PutServiceAccount(w http.ResponseWriter, r *http.Request) {
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
func (m *Iam) DeleteServiceAccount(w http.ResponseWriter, r *http.Request) {
	m.DeleteUser(w, wrapServiceAccount(r))
}

// @Summary Create user
// @Tags users
// @Param user body data.UserCreate true "User"
// @Success 200 {object} data.Response[data.ResponseCreate]
// @Failure 400 {object} data.ResponseError
// @Failure 409 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/users [POST]
func (m *Iam) CreateUser(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	user := data.UserCreate{}
	if err := httputil.Decode(r, &user); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	user.ServiceAccount = isServiceAccount(r)
	user.Disabled = !user.IsActive

	if user.ServiceAccount {
		if user.Details == nil || cast.ToString(user.Details["name"]) == "" || cast.ToString(user.Details["secret"]) == "" {
			httputil.HandleError(w, httputil.NewError("name and secret are required", nil, http.StatusBadRequest))
			return
		}

		if !slices.Contains(user.Alias, cast.ToString(user.Details["name"])) {
			user.Alias = append(user.Alias, cast.ToString(user.Details["name"]))
		}
	}

	if user.Local {
		if user.Details == nil || cast.ToString(user.Details["name"]) == "" || cast.ToString(user.Details["password"]) == "" {
			httputil.HandleError(w, httputil.NewError("name and password is required", nil, http.StatusBadRequest))
			return
		}

		if !slices.Contains(user.Alias, cast.ToString(user.Details["name"])) {
			user.Alias = append(user.Alias, cast.ToString(user.Details["name"]))
		}
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	id, err := m.db.CreateUser(ctx, user.User)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("User already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create user", err, http.StatusInternalServerError))
		return
	}

	// trigger
	m.sync.Trigger(m.ctxService)

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
// @Param permission query []string false "permission" collectionFormat(multi)
// @Param is_active query bool false "is_active"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_permissions query bool false "add permissions"
// @Param add_data query bool false "add data"
// @Param add_scope_roles query bool false "add scope roles"
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.UserExtended]
// @Failure 500 {object} data.ResponseError
// @Router /v1/users [GET]
func (m *Iam) GetUsers(w http.ResponseWriter, r *http.Request) {
	parsedQuery, err := httputil.ParseQuery(r)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot parse query", err, http.StatusBadRequest))
		return
	}

	req := data.GetUserRequest{
		AddRoles:       true,
		ServiceAccount: isServiceAccountPtr(r),
	}

	query := r.URL.Query()
	req.Alias = query.Get("alias")
	req.ID = query.Get("id")

	req.RoleIDs = parsedQuery.GetValues("role_ids")

	req.UID = query.Get("uid")
	req.Name = query.Get("name")
	req.Email = query.Get("email")

	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Permissions = parsedQuery.GetValues("permission")

	if v := query.Get("is_active"); v != "" {
		vBool, _ := strconv.ParseBool(query.Get("is_active"))
		vBool = !vBool
		req.Disabled = &vBool
	}

	if addRoles, err := strconv.ParseBool(query.Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}
	req.AddPermissions, _ = strconv.ParseBool(query.Get("add_permissions"))
	req.AddData, _ = strconv.ParseBool(query.Get("add_data"))
	req.AddScopeRoles, _ = strconv.ParseBool(query.Get("add_scope_roles"))

	req.Limit, req.Offset = getLimitOffset(query)

	users, err := m.db.GetUsers(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get users", err, http.StatusInternalServerError))
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
// @Param is_active query bool false "is_active"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/users/export [GET]
func (m *Iam) ExportUsers(w http.ResponseWriter, r *http.Request) {
	parsedQuery, err := httputil.ParseQuery(r)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot parse query", err, http.StatusBadRequest))
		return
	}

	req := data.GetUserRequest{
		AddRoles:       true,
		ServiceAccount: isServiceAccountPtr(r),
	}

	query := r.URL.Query()
	req.Alias = query.Get("alias")
	req.ID = query.Get("id")

	req.RoleIDs = parsedQuery.GetValues("role_ids")

	req.UID = query.Get("uid")
	req.Name = query.Get("name")
	req.Email = query.Get("email")

	req.Path = query.Get("path")
	req.Method = query.Get("method")

	if v := query.Get("is_active"); v != "" {
		vBool, _ := strconv.ParseBool(query.Get("is_active"))
		vBool = !vBool
		req.Disabled = &vBool
	}

	users, err := m.db.GetUsers(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get users", err, http.StatusInternalServerError))
		return
	}

	// download the result as CSV
	headers := []string{"UID", "Name", "Email", "Roles", "Is Active"}

	userData := make([]map[string]interface{}, 0, len(users.Payload))
	for _, user := range users.Payload {
		if user.Details == nil {
			user.Details = map[string]interface{}{}
		}

		roles := make([]string, 0, len(user.Roles))
		for _, role := range user.Roles {
			roles = append(roles, role.Name)
		}

		userData = append(userData, map[string]interface{}{
			"UID":       user.Details["uid"],
			"Name":      user.Details["name"],
			"Email":     user.Details["email"],
			"Roles":     strings.Join(roles, ","),
			"Is Active": !user.Disabled,
		})
	}

	fileName := time.Now().Format("20060102T1504") + ".csv"
	if req.ServiceAccount != nil && *req.ServiceAccount {
		fileName = "service_" + fileName
	} else {
		fileName = "user_" + fileName
	}

	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, userData, fileName); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot export users", err, http.StatusInternalServerError))
		return
	}
}

// @Summary Get user
// @Tags users
// @Param id path string true "user id"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_permissions query bool false "add permissions"
// @Param add_data query bool false "add data"
// @Success 200 {object} data.Response[data.UserExtended]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/users/{id} [GET]
func (m *Iam) GetUser(w http.ResponseWriter, r *http.Request) {
	req := data.GetUserRequest{
		AddRoles:       true,
		ServiceAccount: isServiceAccountPtr(r),
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	req.ID = id

	query := r.URL.Query()
	if addRoles, err := strconv.ParseBool(query.Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}
	req.AddPermissions, _ = strconv.ParseBool(query.Get("add_permissions"))
	req.AddData, _ = strconv.ParseBool(query.Get("add_data"))

	user, err := m.db.GetUser(req)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get user", err, http.StatusInternalServerError))
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
func (m *Iam) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.DeleteUser(ctx, id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot delete user", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) PatchUser(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	userPatch := data.UserPatch{}
	if err := httputil.Decode(r, &userPatch); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.PatchUser(ctx, id, userPatch); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot patch user", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) PutUser(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	user := data.User{}
	if err := httputil.Decode(r, &user); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	user.ID = id
	user.ServiceAccount = isServiceAccount(r)

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.PutUser(ctx, user); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot put user", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	if user.ServiceAccount {
		httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Service account's data replaced"))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("User's data replaced"))
}

// @Summary Get roles
// @Tags roles
// @Param name query string false "role name"
// @Param description query string false "role description"
// @Param id query string false "role id"
// @Param permission_ids query []string false "role permission ids" collectionFormat(multi)
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param method query string false "request method"
// @Param path query string false "request path"
// @Param permission query []string false "permission" collectionFormat(multi)
// @Param add_permissions query bool false "add permissions default(true)"
// @Param add_roles query bool false "add roles default(true)"
// @Param add_total_users query bool false "add total users default(true)"
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.RoleExtended]
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles [GET]
func (m *Iam) GetRoles(w http.ResponseWriter, r *http.Request) {
	parsedQuery, err := httputil.ParseQuery(r)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot parse query", err, http.StatusBadRequest))
		return
	}

	req := data.GetRoleRequest{
		AddPermissions: true,
		AddRoles:       true,
		AddTotalUsers:  true,
	}

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.PermissionIDs = parsedQuery.GetValues("permission_ids")
	req.RoleIDs = parsedQuery.GetValues("role_ids")
	req.Permissions = parsedQuery.GetValues("permission")
	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Description = query.Get("description")
	req.Limit, req.Offset = getLimitOffset(query)

	if addPermissions, err := strconv.ParseBool(query.Get("add_permissions")); err == nil {
		req.AddPermissions = addPermissions
	}

	if addRoles, err := strconv.ParseBool(query.Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}

	if addTotalUsers, err := strconv.ParseBool(query.Get("add_total_users")); err == nil {
		req.AddTotalUsers = addTotalUsers
	}

	roles, err := m.db.GetRoles(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get roles", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, roles)
}

// @Summary Export roles
// @Tags roles
// @Param name query string false "role name"
// @Param description query string false "role description"
// @Param id query string false "role id"
// @Param permission_ids query []string false "role permission ids" collectionFormat(multi)
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param method query string false "request method"
// @Param path query string false "request path"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles/export [GET]
func (m *Iam) ExportRoles(w http.ResponseWriter, r *http.Request) {
	parsedQuery, err := httputil.ParseQuery(r)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot parse query", err, http.StatusBadRequest))
		return
	}

	req := data.GetRoleRequest{
		AddPermissions: true,
		AddRoles:       true,
		AddTotalUsers:  true,
	}

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.PermissionIDs = parsedQuery.GetValues("permission_ids")
	req.RoleIDs = parsedQuery.GetValues("role_ids")
	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Description = query.Get("description")
	req.Limit, req.Offset = getLimitOffset(query)

	if addPermissions, err := strconv.ParseBool(query.Get("add_permissions")); err == nil {
		req.AddPermissions = addPermissions
	}

	if addRoles, err := strconv.ParseBool(query.Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}

	if addTotalUsers, err := strconv.ParseBool(query.Get("add_total_users")); err == nil {
		req.AddTotalUsers = addTotalUsers
	}

	roles, err := m.db.GetRoles(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get roles", err, http.StatusInternalServerError))
		return
	}

	// download the result as CSV
	headers := []string{"Name", "Permissions", "Roles", "Description", "Total Users"}

	roleData := make([]map[string]interface{}, 0, len(roles.Payload))
	for _, role := range roles.Payload {
		permissions := make([]string, 0, len(role.Permissions))
		for _, permission := range role.Permissions {
			permissions = append(permissions, permission.Name)
		}

		permissionByte, err := json.Marshal(permissions)
		if err != nil {
			httputil.HandleError(w, httputil.NewError("Cannot marshal permissions", err, http.StatusInternalServerError))
			return
		}

		roles := make([]string, 0, len(role.Roles))
		for _, r := range role.Roles {
			roles = append(roles, r.Name)
		}

		rolesByte, err := json.Marshal(roles)
		if err != nil {
			httputil.HandleError(w, httputil.NewError("Cannot marshal roles", err, http.StatusInternalServerError))
			return
		}

		roleData = append(roleData, map[string]interface{}{
			"Name":        role.Name,
			"Permissions": string(permissionByte),
			"Roles":       string(rolesByte),
			"Description": role.Description,
			"Total Users": humanize.Comma(int64(role.TotalUsers)),
		})
	}

	fileName := "roles_" + time.Now().Format("20060102T1504") + ".csv"

	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, roleData, fileName); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot export roles", err, http.StatusInternalServerError))
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
func (m *Iam) CreateRole(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	role := data.Role{}
	if err := httputil.Decode(r, &role); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	if role.Name == "" {
		httputil.HandleError(w, httputil.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	id, err := m.db.CreateRole(ctx, role)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Role already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create role", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) GetRole(w http.ResponseWriter, r *http.Request) {
	req := data.GetRoleRequest{
		AddPermissions: true,
		AddRoles:       true,
		AddTotalUsers:  true,
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	req.ID = id

	role, err := m.db.GetRole(req)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.RoleExtended]{
		Payload: role,
	})
}

// @Summary Set role relation
// @Tags roles
// @Param relation body map[string]data.RoleRelation true "RoleRelation"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles/relation [PUT]
func (m *Iam) PutRoleRelation(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	relation := map[string]data.RoleRelation{}
	if err := httputil.Decode(r, &relation); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.PutRoleRelation(ctx, relation); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot patch role", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.ResponseMessage{
		Message: &data.Message{
			Text: "Roles relation updated",
		},
	})
}

// @Summary Get role relation (dump)
// @Tags roles
// @Success 200 {object} map[string]data.RoleRelation
// @Failure 500 {object} data.ResponseError
// @Router /v1/roles/relation [GET]
func (m *Iam) GetRoleRelation(w http.ResponseWriter, _ *http.Request) {
	relation, err := m.db.GetRoleRelation()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get role relation", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, relation)
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
func (m *Iam) PatchRole(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	rolePatch := data.RolePatch{}
	if err := httputil.Decode(r, &rolePatch); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.PatchRole(ctx, id, rolePatch); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot patch role", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) PutRole(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	role := data.Role{}
	if err := httputil.Decode(r, &role); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	if role.Name == "" {
		httputil.HandleError(w, httputil.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	role.ID = id

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.PutRole(ctx, role); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot put role", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) DeleteRole(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.DeleteRole(ctx, id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot delete role", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Role deleted"))
}

// @Summary Get permissions
// @Tags permissions
// @Param id query string false "permission id"
// @Param name query string false "permission name"
// @Param description query string false "permission description"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Param add_roles query bool false "add roles default(true)"
// @Param limit query int false "limit" default(20)
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.Permission]
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions [GET]
func (m *Iam) GetPermissions(w http.ResponseWriter, r *http.Request) {
	req := data.GetPermissionRequest{
		AddRoles: true,
	}

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Description = query.Get("description")

	if addRoles, err := strconv.ParseBool(query.Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}

	for k, v := range query {
		if strings.HasPrefix(k, "data.") {
			if req.Data == nil {
				req.Data = map[string]string{}
			}

			req.Data[strings.TrimPrefix(k, "data.")] = v[0]
		}
	}

	req.Limit, req.Offset = getLimitOffset(query)

	permissions, err := m.db.GetPermissions(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get permissions", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, permissions)
}

// @Summary Export permissions
// @Tags permissions
// @Param id query string false "permission id"
// @Param name query string false "permission name"
// @Param description query string false "permission description"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/export [GET]
func (m *Iam) ExportPermissions(w http.ResponseWriter, r *http.Request) {
	var req data.GetPermissionRequest

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Description = query.Get("description")

	permissions, err := m.db.GetPermissions(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get permissions", err, http.StatusInternalServerError))
		return
	}

	// download the result as CSV
	headers := []string{"Name", "Description", "Resources"}

	permissionData := make([]map[string]interface{}, 0, len(permissions.Payload))
	for _, permission := range permissions.Payload {
		resourceByte, err := json.Marshal(permission.Resources)
		if err != nil {
			httputil.HandleError(w, httputil.NewError("Cannot marshal resources", err, http.StatusInternalServerError))
			return
		}

		permissionData = append(permissionData, map[string]interface{}{
			"Name":        permission.Name,
			"Resources":   string(resourceByte),
			"Description": permission.Description,
		})
	}

	fileName := "permissions_" + time.Now().Format("20060102T1504") + ".csv"

	if err := httputil.NewExport(httputil.ExportTypeCSV).ExportHTTP(w, headers, permissionData, fileName); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot export permissions", err, http.StatusInternalServerError))
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
func (m *Iam) CreatePermission(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	permission := data.Permission{}
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	id, err := m.db.CreatePermission(ctx, permission)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Permission already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create permission", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreate]{
		Message: &data.Message{
			Text: "Permission created",
		},
		Payload: data.ResponseCreate{
			ID: id,
		},
	})
}

// @Summary Create permission bulk
// @Tags permissions
// @Param permission body []data.Permission true "Permission"
// @Success 200 {object} data.Response[data.ResponseCreateBulk]
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/bulk [POST]
func (m *Iam) CreatePermissionBulk(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	permissions := []data.Permission{}
	if err := httputil.Decode(r, &permissions); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	ids, err := m.db.CreatePermissions(ctx, permissions)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot create permission", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.Response[data.ResponseCreateBulk]{
		Message: &data.Message{
			Text: "Permissions created",
		},
		Payload: data.ResponseCreateBulk{
			IDs: ids,
		},
	})
}

// @Summary Get permission
// @Tags permissions
// @Param id path string true "permission name"
// @Param add_roles query bool false "add roles default(true)"
// @Success 200 {object} data.Response[data.Permission]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/{id} [GET]
func (m *Iam) GetPermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	req := data.GetPermissionRequest{
		AddRoles: true,
	}
	req.ID = id

	query := r.URL.Query()
	if addRoles, err := strconv.ParseBool(query.Get("add_roles")); err == nil {
		req.AddRoles = addRoles
	}

	permission, err := m.db.GetPermission(req)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.PermissionExtended]{
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
func (m *Iam) PatchPermission(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	permission := data.PermissionPatch{}
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.PatchPermission(ctx, id, permission); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Permission already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot patch permission", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) PutPermission(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	permission := data.Permission{}
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	permission.ID = id

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.PutPermission(ctx, permission); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot put permission", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) DeletePermission(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.DeletePermission(ctx, id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot delete permission", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Permission deleted"))
}

// @Summary Delete permission bulk
// @Tags permissions
// @Param permission body []data.Permission true "Permission"
// @Success 200 {object} data.Response[[]data.IDName]
// @Failure 500 {object} data.ResponseError
// @Router /v1/permissions/keep [POST]
func (m *Iam) KeepPermissionBulk(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	permissions := []data.NameRequest{}
	if err := httputil.Decode(r, &permissions); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	permissionsMap := make(map[string]struct{}, len(permissions))
	for _, p := range permissions {
		permissionsMap[p.Name] = struct{}{}
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	idName, err := m.db.KeepPermissions(ctx, permissionsMap)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot delete permission", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.Response[[]data.IDName]{
		Message: &data.Message{
			Text: "Permissions deleted",
		},
		Payload: idName,
	})

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Permission deleted"))
}

// @Summary Post check
// @Tags check
// @Param check body data.CheckRequest true "Check"
// @Success 200 {object} data.CheckResponse
// @Failure 400 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/check [POST]
func (m *Iam) PostCheck(w http.ResponseWriter, r *http.Request) {
	body := data.CheckRequest{}
	if err := httputil.Decode(r, &body); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	resp, err := m.db.Check(body)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot check", err, http.StatusInternalServerError))
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
func (m *Iam) PostCheckUser(w http.ResponseWriter, r *http.Request) {
	user := r.Header.Get("X-User")
	if user == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusBadRequest))
		return
	}

	body := data.CheckRequestUser{}
	if err := httputil.Decode(r, &body); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	resp, err := m.db.Check(data.CheckRequest{
		Alias:  user,
		Path:   body.Path,
		Method: body.Method,
		Host:   body.Host,
	})
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot check", err, http.StatusInternalServerError))
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
func (m *Iam) LdapCreateGroupMaps(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	lmap := data.LMap{}
	if err := httputil.Decode(r, &lmap); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	if lmap.Name == "" {
		httputil.HandleError(w, httputil.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.CreateLMap(ctx, lmap); err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Map already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create ldap map", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) LdapGetGroupMaps(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	req := data.GetLMapRequest{}

	query := r.URL.Query()
	req.Name = query.Get("name")
	req.RoleIDs = query["role_ids"]
	req.Limit, req.Offset = getLimitOffset(query)

	maps, err := m.db.GetLMaps(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get ldap maps", err, http.StatusInternalServerError))
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
func (m *Iam) LdapGetGroupMap(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.HandleError(w, httputil.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	maps, err := m.db.GetLMap(name)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Map not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get ldap map", err, http.StatusInternalServerError))
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
func (m *Iam) LdapPutGroupMaps(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.HandleError(w, httputil.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	lmap := data.LMap{}
	if err := httputil.Decode(r, &lmap); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	lmap.Name = name

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.PutLMap(ctx, lmap); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Map not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot put ldap map", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

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
func (m *Iam) LdapDeleteGroupMaps(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.HandleError(w, httputil.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	ctx := data.WithContextUserName(m.ctxService, getUserName(r))

	if err := m.db.DeleteLMap(ctx, name); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Map not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot delete ldap map", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Map deleted"))
}

// @Summary Get current user's info
// @Tags info
// @Param alias query string true "User alias"
// @Param data query bool false "add role data"
// @Success 200 {object} data.Response[data.UserInfo]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/info [GET]
func (m *Iam) Info(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	alias := query.Get("alias")

	if alias == "" {
		httputil.HandleError(w, httputil.NewError("Alias is required", nil, http.StatusBadRequest))
		return
	}

	getData, _ := strconv.ParseBool(query.Get("data"))

	user, err := m.db.GetUser(data.GetUserRequest{Alias: alias, AddRoles: true, AddPermissions: true, AddData: getData, Sanitize: true})
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get user", err, http.StatusInternalServerError))
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
		Data:        user.Data,
		IsActive:    !user.Disabled,
	}

	httputil.JSON(w, http.StatusOK, data.Response[data.UserInfo]{Payload: userInfo})
}

// @Summary Get current user's info
// @Tags public
// @Param X-User header string false "User alias, X-User is authorized user auto adds"
// @Param data query bool false "add role data"
// @Success 200 {object} data.Response[data.UserInfo]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /info [GET]
func (m *Iam) InfoUser(w http.ResponseWriter, r *http.Request) {
	xUser := r.Header.Get("X-User")

	if xUser == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusBadRequest))
		return
	}

	var getData bool
	if v := r.Header.Get("X-Data"); v != "" {
		vBool, _ := strconv.ParseBool(v)
		getData = vBool
	}

	if v := r.URL.Query().Get("data"); v != "" {
		vBool, _ := strconv.ParseBool(v)
		getData = vBool
	}

	user, err := m.db.GetUser(data.GetUserRequest{Alias: xUser, AddRoles: true, AddPermissions: true, AddData: getData, Sanitize: true})
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found ["+xUser+"]", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get user", err, http.StatusInternalServerError))
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
		Data:        user.Data,
		IsActive:    !user.Disabled,
	}

	httputil.JSON(w, http.StatusOK, data.Response[data.UserInfo]{Payload: userInfo})
}

// @Summary Backup Database
// @Tags backup
// @Param since query number false "since txid"
// @Success 200
// @Failure 500 {object} data.ResponseError
// @Router /v1/backup [GET]
func (m *Iam) Backup(w http.ResponseWriter, r *http.Request) {
	var since uint64
	sinceStr := r.URL.Query().Get("since")
	if sinceStr != "" {
		var err error
		since, err = strconv.ParseUint(sinceStr, 10, 64)
		if err != nil {
			httputil.HandleError(w, httputil.NewError("Cannot parse since", err, http.StatusBadRequest))
			return
		}
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=iam_"+time.Now().Format("2006-01-02_15-04-05")+".db")

	backupVersion, err := m.db.Backup(w, since)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot backup", err, http.StatusInternalServerError))
		return
	}

	slog.Info("database backup", slog.Uint64("since", since), slog.Uint64("version", backupVersion))
}

// @Summary Restore Database
// @Tags backup
// @Param file formData file true "Backup file"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/restore [POST]
func (m *Iam) Restore(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get file", err, http.StatusBadRequest))
		return
	}
	if file == nil {
		httputil.HandleError(w, httputil.NewError("File is required", nil, http.StatusBadRequest))
		return
	}

	defer file.Close()

	logi.Ctx(m.ctxService).Info("restoring database",
		slog.String("file_name", fileHeader.Filename),
		slog.String("file_size", humanize.Bytes(uint64(fileHeader.Size))),
		slog.String("by", getUserName(r)),
	)

	if err := m.db.Restore(file); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot restore", err, http.StatusInternalServerError))
		return
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Database restored"))
}

// @Summary Get version
// @Tags backup
// @Success 200 {object} data.ResponseVersion
// @Router /v1/version [GET]
func (m *Iam) Version(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, data.ResponseVersion{
		Version: m.db.Version(),
	})
}

// @Summary Sync with write API
// @Tags backup
// @Param X-Sync-Version header string false "Sync version"
// @Success 200 {object} data.ResponseMessage
// @Failure 400 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/sync [POST]
func (m *Iam) Sync(w http.ResponseWriter, r *http.Request) {
	var versionNumber uint64
	version := r.Header.Get("X-Sync-Version")
	if version != "" {
		var err error
		versionNumber, err = strconv.ParseUint(version, 10, 64)
		if err != nil {
			httputil.HandleError(w, httputil.NewError("Cannot parse version", err, http.StatusBadRequest))
			return
		}
	}

	if err := m.sync.Sync(m.ctxService, versionNumber); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot sync", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Synced"))
}

// @Summary Get dashboard info
// @Tags dashboard
// @Success 200 {object} data.Response[data.Dashboard]
// @Failure 500 {object} data.ResponseError
// @Router /v1/dashboard [GET]
func (m *Iam) Dashboard(w http.ResponseWriter, _ *http.Request) {
	dashboard, err := m.db.Dashboard()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get dashboard", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[*data.Dashboard]{Payload: dashboard})
}
