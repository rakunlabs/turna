package rebac

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
)

// @Title ReBAC API
// @BasePath /
// @description ReBAC API
//
//go:generate swag init -pd -d ./ -g api.go --ot json -o ./files
func (m *Rebac) MuxSet(prefix string) *chi.Mux {
	mux := chi.NewMux()

	prefix = strings.TrimRight(prefix, "/")

	mux.Get(prefix+"/users", m.GetUsers)
	mux.Post(prefix+"/users", m.CreateUser)
	mux.Get(prefix+"/users/{id}", m.GetUser)
	mux.Patch(prefix+"/users/{id}", m.PatchUser)
	mux.Put(prefix+"/users/{id}", m.PutUser)
	mux.Delete(prefix+"/users/{id}", m.DeleteUser)

	mux.Get(prefix+"/roles", m.GetRoles)
	mux.Post(prefix+"/roles", m.CreateRole)
	mux.Get(prefix+"/roles/{id}", m.GetRole)
	mux.Put(prefix+"/roles/{id}", m.PutRole)
	mux.Delete(prefix+"/roles/{id}", m.DeleteRole)

	mux.Get(prefix+"/permissions", m.GetPermissions)
	mux.Post(prefix+"/permissions", m.CreatePermission)
	mux.Get(prefix+"/permissions/{id}", m.GetPermission)
	mux.Put(prefix+"/permissions/{id}", m.PutPermission)
	mux.Delete(prefix+"/permissions/{id}", m.DeletePermission)

	mux.Get(prefix+"/ldap/users/{uid}", m.LdapGetUsers)
	mux.Get(prefix+"/ldap/groups", m.LdapGetGroups)
	mux.Post(prefix+"/ldap/sync", m.LdapSyncGroups)

	mux.Get(prefix+"/ldap/maps", m.LdapGetGroupMaps)
	mux.Post(prefix+"/ldap/maps", m.LdapCreateGroupMaps)
	mux.Get(prefix+"/ldap/maps/{name}", m.LdapGetGroupMap)
	mux.Put(prefix+"/ldap/maps/{name}", m.LdapPutGroupMaps)
	mux.Delete(prefix+"/ldap/maps/{name}", m.LdapDeleteGroupMaps)

	mux.Post(prefix+"/check", m.PostCheck)

	mux.Get(prefix+"/ui/info", m.Info)
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

func (m *Rebac) Info(w http.ResponseWriter, _ *http.Request) {
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
// @Router /users [POST]
func (m *Rebac) CreateUser(w http.ResponseWriter, r *http.Request) {
	user := data.User{}
	if err := httputil.Decode(r, &user); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	user.ID = ulid.Make().String()
	if err := m.db.CreateUser(user); err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("User already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create user", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "User created",
	})
}

// @Summary Get users
// @Tags users
// @Param alias query string false "user alias"
// @Param id query string false "user id"
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.User]
// @Failure 500 {object} httputil.Error
// @Router /users [GET]
func (m *Rebac) GetUsers(w http.ResponseWriter, r *http.Request) {
	var req data.GetUserRequest

	query := r.URL.Query()
	req.Alias = query.Get("alias")
	req.ID = query.Get("id")
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
// @Success 200 {object} data.User
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /users/{id} [GET]
func (m *Rebac) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.HandleError(w, httputil.NewError("id is required", nil, http.StatusBadRequest))
		return
	}

	user, err := m.db.GetUser(data.GetUserRequest{ID: id})
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
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /users/{id} [POST]
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
}

// @Summary Patch user
// @Tags users
// @Param id path string true "user id"
// @Param user body data.User true "User"
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /users/{id} [PATCH]
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
}

// @Summary Put user
// @Tags users
// @Param id path string true "user id"
// @Param user body data.User true "User"
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /users/{id} [PUT]
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
}

// @Summary Get roles
// @Tags roles
// @Param name query string false "role name"
// @Param id query string false "role id"
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.Role]
// @Failure 500 {object} httputil.Error
// @Router /roles [GET]
func (m *Rebac) GetRoles(w http.ResponseWriter, r *http.Request) {
	var req data.GetRoleRequest

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
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
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 409 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /roles [POST]
func (m *Rebac) CreateRole(w http.ResponseWriter, r *http.Request) {
	role := data.Role{}
	if err := httputil.Decode(r, &role); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	role.ID = ulid.Make().String()
	if err := m.db.CreateRole(role); err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Role already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create role", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Role created",
	})
}

// @Summary Get role
// @Tags roles
// @Param id path string true "role ID"
// @Success 200 {object} data.Role
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /roles/{id} [GET]
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

// @Summary Put role
// @Tags roles
// @Param id path string true "role ID"
// @Param role body data.Role true "Role"
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /roles/{id} [PUT]
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
}

// @Summary Delete role
// @Tags roles
// @Param id path string true "role name"
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /roles/{id} [DELETE]
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
}

// @Summary Get permissions
// @Tags permissions
// @Param id query string false "permission id"
// @Param name query string false "permission name"
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} data.Response[[]data.Permission]
// @Failure 500 {object} httputil.Error
// @Router /permissions [GET]
func (m *Rebac) GetPermissions(w http.ResponseWriter, r *http.Request) {
	var req data.GetPermissionRequest

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
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
// @Success 200 {object} httputil.Response
// @Failure 400 {object} httputil.Error
// @Failure 409 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /permissions [POST]
func (m *Rebac) CreatePermission(w http.ResponseWriter, r *http.Request) {
	permission := data.Permission{}
	if err := httputil.Decode(r, &permission); err != nil {
		httputil.HandleError(w, httputil.NewError("Cannot decode request", err, http.StatusBadRequest))
		return
	}

	permission.ID = ulid.Make().String()
	if err := m.db.CreatePermission(permission); err != nil {
		if errors.Is(err, data.ErrConflict) {
			httputil.HandleError(w, httputil.NewError("Permission already exists", err, http.StatusConflict))
			return
		}

		httputil.HandleError(w, httputil.NewError("Cannot create permission", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{
		Msg: "Permission created",
	})
}

// @Summary Get permission
// @Tags permissions
// @Param id path string true "permission name"
// @Success 200 {object} data.Permission
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /permissions/{id} [GET]
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

// @Summary Put permission
// @Tags permissions
// @Param id path string true "permission ID"
// @Param permission body data.Permission true "Permission"
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /permissions/{id} [PUT]
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
}

// @Summary Delete permission
// @Tags permissions
// @Param id path string true "permission name"
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /permissions/{id} [DELETE]
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
}

// @Summary Post check
// @Tags check
// @Param check body data.CheckRequest true "Check"
// @Success 200 {object} data.CheckResponse
// @Failure 400 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /check [POST]
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
// @Router /ldap/maps [POST]
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
// @Param id query string false "map id"
// @Param name query string false "map name"
// @Param limit query int false "limit"
// @Param offset query int false "offset"
// @Success 200 {object} []data.GetLMapRequest
// @Failure 500 {object} httputil.Error
// @Router /ldap/maps [GET]
func (m *Rebac) LdapGetGroupMaps(w http.ResponseWriter, r *http.Request) {
	req := data.GetLMapRequest{}

	query := r.URL.Query()
	req.ID = query.Get("id")
	req.Name = query.Get("name")
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
// @Router /ldap/maps/{name} [GET]
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
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /ldap/maps/{name} [PUT]
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
}

// @Summary Delete LDAP Map
// @Tags ldap
// @Param name path string true "map name"
// @Success 204
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /ldap/maps/{name} [DELETE]
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
}
