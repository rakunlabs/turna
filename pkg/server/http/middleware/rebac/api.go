package rebac

import (
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
	mux.Get(prefix+"/v1/users/{id}", m.GetUser)
	mux.Patch(prefix+"/v1/users/{id}", m.PatchUser)
	mux.Put(prefix+"/v1/users/{id}", m.PutUser)
	mux.Delete(prefix+"/v1/users/{id}", m.DeleteUser)

	mux.Get(prefix+"/v1/roles", m.GetRoles)
	mux.Post(prefix+"/v1/roles", m.CreateRole)
	mux.Get(prefix+"/v1/roles/{id}", m.GetRole)
	mux.Patch(prefix+"/v1/roles/{id}", m.PatchRole)
	mux.Put(prefix+"/v1/roles/{id}", m.PutRole)
	mux.Delete(prefix+"/v1/roles/{id}", m.DeleteRole)

	mux.Get(prefix+"/v1/permissions", m.GetPermissions)
	mux.Post(prefix+"/v1/permissions", m.CreatePermission)
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

	mux.Get(prefix+"/v1/backup", m.Backup)
	mux.Post(prefix+"/v1/restore", m.Restore)

	mux.Get(prefix+"/ui/info", m.UIInfo)
	mux.Handle(prefix+"/swagger/*", m.swaggerFS)
	mux.Handle(prefix+"/ui/*", m.uiFS)

	return mux
}

func getLimitOffset(v url.Values) (limit, offset int64) {
	limit, _ = strconv.ParseInt(v.Get("limit"), 10, 64)
	if limit == 0 {
		limit = 20
	}

	offset, _ = strconv.ParseInt(v.Get("offset"), 10, 64)

	return
}

func (m *Rebac) UIInfo(w http.ResponseWriter, _ *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]interface{}{
		"prefix_path": m.PrefixPath,
	})
}

// @Summary Create user
// @Tags users
// @Param user body data.User true "User"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 409 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/users [POST]
func (m *Rebac) CreateUser(w http.ResponseWriter, r *http.Request) {
	user := data.User{}
	if err := httputil.Decode(r, &user); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	id, err := m.db.CreateUser(user)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("User already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create user", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.ResponseCreate{
		ID:      id,
		Message: "User created",
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
// @Param extend query bool false "extend datas"
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.UserExtended]
// @Failure 500 {object} httputil.Error
// @Router /v1/users [GET]
func (m *Rebac) GetUsers(w http.ResponseWriter, r *http.Request) {
	var req data.GetUserRequest

	query := r.URL.Query()
	req.Alias = query.Get("alias")
	req.ID = query.Get("id")

	req.RoleIDs = query["role_ids"]

	req.UID = query.Get("uid")
	req.Name = query.Get("name")
	req.Email = query.Get("email")

	req.Path = query.Get("path")
	req.Method = query.Get("method")

	req.Extend, _ = strconv.ParseBool(r.URL.Query().Get("extend"))

	req.Limit, req.Offset = getLimitOffset(query)

	users, err := m.db.GetUsers(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get users", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, users)
}

// @Summary Get user
// @Tags users
// @Param id path string true "user id"
// @Param extend query bool false "extend datas"
// @Success 200 {object} data.UserExtended
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/users/{id} [GET]
func (m *Rebac) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	extend, _ := strconv.ParseBool(r.URL.Query().Get("extend"))

	user, err := m.db.GetUser(data.GetUserRequest{ID: id, Extend: extend})
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get user", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, user)
}

// @Summary Delete user
// @Tags users
// @Param id path string true "user id"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/users/{id} [POST]
func (m *Rebac) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.db.DeleteUser(id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot delete user", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "User deleted",
	})
}

// @Summary Patch user
// @Tags users
// @Param id path string true "user id"
// @Param user body data.User true "User"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/users/{id} [PATCH]
func (m *Rebac) PatchUser(w http.ResponseWriter, r *http.Request) {
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

	if err := m.db.PatchUser(user); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot patch user", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "User's data patched",
	})
}

// @Summary Put user
// @Tags users
// @Param id path string true "user id"
// @Param user body data.User true "User"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/users/{id} [PUT]
func (m *Rebac) PutUser(w http.ResponseWriter, r *http.Request) {
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

	if err := m.db.PutUser(user); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot put user", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "User's data replaced",
	})
}

// @Summary Get roles
// @Tags roles
// @Param name query string false "role name"
// @Param id query string false "role id"
// @Param permission_ids query []string false "role permission ids" collectionFormat(multi)
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param method query string false "request method"
// @Param path query string false "request path"
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.Role]
// @Failure 500 {object} httputil.Error
// @Router /v1/roles [GET]
func (m *Rebac) GetRoles(w http.ResponseWriter, r *http.Request) {
	var req data.GetRoleRequest

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
	req.PermissionIDs = query["permission_ids"]
	req.RoleIDs = query["role_ids"]
	req.Path = query.Get("path")
	req.Method = query.Get("method")
	req.Limit, req.Offset = getLimitOffset(query)

	roles, err := m.db.GetRoles(req)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot get roles", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, roles)
}

// @Summary Create role
// @Tags roles
// @Param role body data.Role true "Role"
// @Success 200 {object} data.ResponseCreate
// @Failure 400 {object} httputil.Error
// @Failure 409 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/roles [POST]
func (m *Rebac) CreateRole(w http.ResponseWriter, r *http.Request) {
	role := data.Role{}
	if err := httputil.Decode(r, &role); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	id, err := m.db.CreateRole(role)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Role already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.ResponseCreate{
		ID:      id,
		Message: "Role created",
	})
}

// @Summary Get role
// @Tags roles
// @Param id path string true "role ID"
// @Success 200 {object} data.Role
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/roles/{id} [GET]
func (m *Rebac) GetRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	role, err := m.db.GetRole(id)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, role)
}

// @Summary Patch role
// @Tags roles
// @Param id path string true "role ID"
// @Param role body data.Role true "Role"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/roles/{id} [PATCH]
func (m *Rebac) PatchRole(w http.ResponseWriter, r *http.Request) {
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

	role.ID = id

	if err := m.db.PatchRole(role); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot patch role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Role's data patched",
	})
}

// @Summary Put role
// @Tags roles
// @Param id path string true "role ID"
// @Param role body data.Role true "Role"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/roles/{id} [PUT]
func (m *Rebac) PutRole(w http.ResponseWriter, r *http.Request) {
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

	role.ID = id

	if err := m.db.PutRole(role); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot put role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Role's data replaced",
	})
}

// @Summary Delete role
// @Tags roles
// @Param id path string true "role name"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/roles/{id} [DELETE]
func (m *Rebac) DeleteRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.db.DeleteRole(id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Role not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot delete role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Role deleted",
	})
}

// @Summary Get permissions
// @Tags permissions
// @Param id query string false "permission id"
// @Param name query string false "permission name"
// @Param path query string false "request path"
// @Param method query string false "request method"
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.Permission]
// @Failure 500 {object} httputil.Error
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
		httputil.HandleError(w, httputil.NewError("Cannot get permissions", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, permissions)
}

// @Summary Create permission
// @Tags permissions
// @Param permission body data.Permission true "Permission"
// @Success 200 {object} data.ResponseCreate
// @Failure 400 {object} httputil.Error
// @Failure 409 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/permissions [POST]
func (m *Rebac) CreatePermission(w http.ResponseWriter, r *http.Request) {
	permission := data.Permission{}
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	id, err := m.db.CreatePermission(permission)
	if err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Permission already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.ResponseCreate{
		ID:      id,
		Message: "Permission created",
	})
}

// @Summary Get permission
// @Tags permissions
// @Param id path string true "permission name"
// @Success 200 {object} data.Permission
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/permissions/{id} [GET]
func (m *Rebac) GetPermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	permission, err := m.db.GetPermission(id)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, permission)
}

// @Summary Patch permission
// @Tags permissions
// @Param id path string true "permission ID"
// @Param permission body data.Permission true "Permission"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/permissions/{id} [PATCH]
func (m *Rebac) PatchPermission(w http.ResponseWriter, r *http.Request) {
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

	if err := m.db.PatchPermission(permission); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot patch permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Permission's data patched",
	})
}

// @Summary Put permission
// @Tags permissions
// @Param id path string true "permission ID"
// @Param permission body data.Permission true "Permission"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/permissions/{id} [PUT]
func (m *Rebac) PutPermission(w http.ResponseWriter, r *http.Request) {
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

	if err := m.db.PutPermission(permission); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot put permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Permission's data replaced",
	})
}

// @Summary Delete permission
// @Tags permissions
// @Param id path string true "permission name"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/permissions/{id} [DELETE]
func (m *Rebac) DeletePermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.db.DeletePermission(id); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Permission not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot delete permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Permission deleted",
	})
}

// @Summary Post check
// @Tags check
// @Param check body data.CheckRequest true "Check"
// @Success 200 {object} data.CheckResponse
// @Failure 400 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/check [POST]
func (m *Rebac) PostCheck(w http.ResponseWriter, r *http.Request) {
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

// @Summary Create LDAP Map
// @Tags ldap
// @Param map body data.LMap true "Map"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 409 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/maps [POST]
func (m *Rebac) LdapCreateGroupMaps(w http.ResponseWriter, r *http.Request) {
	lmap := data.LMap{}
	if err := httputil.Decode(r, &lmap); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	if err := m.db.CreateLMap(lmap); err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Map already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create ldap map", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Map created",
	})
}

// @Summary Get LDAP Maps
// @Tags ldap
// @Param name query string false "map name"
// @Param role_ids query []string false "role ids" collectionFormat(multi)
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} []data.GetLMapRequest
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/maps [GET]
func (m *Rebac) LdapGetGroupMaps(w http.ResponseWriter, r *http.Request) {
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
// @Success 200 {object} data.LMap
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/maps/{name} [GET]
func (m *Rebac) LdapGetGroupMap(w http.ResponseWriter, r *http.Request) {
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

	httputil.JSON(w, http.StatusOK, maps)
}

// @Summary Put LDAP Map
// @Tags ldap
// @Param name path string true "map name"
// @Param map body data.LMap true "Map"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/maps/{name} [PUT]
func (m *Rebac) LdapPutGroupMaps(w http.ResponseWriter, r *http.Request) {
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

	if err := m.db.PutLMap(lmap); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Map not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot put ldap map", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Map replaced",
	})
}

// @Summary Delete LDAP Map
// @Tags ldap
// @Param name path string true "map name"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/maps/{name} [DELETE]
func (m *Rebac) LdapDeleteGroupMaps(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httputil.HandleError(w, httputil.NewError("name is required", nil, http.StatusBadRequest))
		return
	}

	if err := m.db.DeleteLMap(name); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("Map not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot delete ldap map", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Map deleted",
	})
}

// @Summary Get current user's info
// @Tags info
// @Param X-User header string true "User alias"
// @Param datas query bool false "add role datas"
// @Success 200 {object} data.UserInfo
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/info [GET]
func (m *Rebac) Info(w http.ResponseWriter, r *http.Request) {
	xUser := r.Header.Get("X-User")

	if xUser == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusBadRequest))
		return
	}

	user, err := m.db.GetUser(data.GetUserRequest{Alias: xUser, Extend: true})
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, httputil.NewError("User not found", err, http.StatusNotFound))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot get user", err, http.StatusInternalServerError))
		return
	}

	var datas []interface{}

	if v, _ := strconv.ParseBool(r.URL.Query().Get("datas")); v {
		datas = user.Datas
	}

	userInfo := data.UserInfo{
		Details:     user.Details,
		Roles:       user.Roles,
		Permissions: user.Permissions,
		Datas:       datas,
	}

	httputil.JSON(w, http.StatusOK, userInfo)
}

// @Summary Backup Database
// @Tags backup
// @Param since query number false "since txid"
// @Success 200
// @Failure 500 {object} httputil.Error
// @Router /v1/backup [GET]
func (m *Rebac) Backup(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Disposition", "attachment; filename=rebac_"+time.Now().Format("2006-01-02_15-04-05")+".db")
	if err := m.db.Backup(w, since); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot backup", err, http.StatusInternalServerError))
		return
	}
}

// @Summary Restore Database
// @Tags backup
// @Param file formData file true "Backup file"
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/restore [POST]
func (m *Rebac) Restore(w http.ResponseWriter, r *http.Request) {
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

	slog.Info("restoring database",
		slog.String("file_name", fileHeader.Filename),
		slog.String("file_size", humanize.Bytes(uint64(fileHeader.Size))),
	)

	if err := m.db.Restore(file); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot restore", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Database restored",
	})
}
