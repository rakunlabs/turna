package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/xhit/go-str2duration/v2"
)

type APIKeyCreateRequest struct {
	// UserID owns the key. Empty keeps the legacy X-User self-service behavior.
	UserID string `json:"user_id"`
	// Name is a user-facing label for the key.
	Name string `json:"name"`
	// ExpiresIn is a duration string (e.g. "720h", "30d"); empty means no expiry
	// unless the api_key setting enforces a max lifetime.
	ExpiresIn     string         `json:"expires_in"`
	RoleIDs       *[]string      `json:"role_ids"`
	PermissionIDs *[]string      `json:"permission_ids"`
	Details       map[string]any `json:"details"`
	Disabled      bool           `json:"disabled"`
}

type APIKeyUpdateRequest struct {
	Name          *string         `json:"name"`
	RoleIDs       *[]string       `json:"role_ids"`
	PermissionIDs *[]string       `json:"permission_ids"`
	Details       *map[string]any `json:"details"`
	Disabled      *bool           `json:"disabled"`
}

type APIKeyCreateResponse struct {
	ID string `json:"id"`
	// Key is shown exactly once; only its hash is stored.
	Key       string `json:"key"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// xUser resolves the X-User header to a user, writing the error response
// itself and returning nil when that fails.
func (m *Auth) xUser(w http.ResponseWriter, r *http.Request) *data.UserExtended {
	alias := r.Header.Get("X-User")
	if alias == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusUnauthorized))
		return nil
	}

	user, err := m.cache.GetUser(data.GetUserRequest{Alias: alias})
	if err != nil {
		httputil.HandleError(w, httputil.NewError("user not found", err, http.StatusNotFound))
		return nil
	}

	return user
}

func cleanAPIKeyIDs(ids []string) []string {
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			clean = append(clean, id)
		}
	}

	return slicesUnique(clean)
}

func ownerAPIKeyAccess(owner *data.UserExtended) (map[string]struct{}, map[string]struct{}, []string, []string) {
	roleSet := make(map[string]struct{}, len(owner.Roles))
	permissionSet := make(map[string]struct{}, len(owner.Permissions))
	roleIDs := make([]string, 0, len(owner.Roles))
	permissionIDs := make([]string, 0, len(owner.Permissions))

	for _, role := range owner.Roles {
		roleSet[role.ID] = struct{}{}
		roleIDs = append(roleIDs, role.ID)
	}
	for _, permission := range owner.Permissions {
		permissionSet[permission.ID] = struct{}{}
		permissionIDs = append(permissionIDs, permission.ID)
	}

	return roleSet, permissionSet, slicesUnique(roleIDs), slicesUnique(permissionIDs)
}

func apiKeyAccessForOwner(owner *data.UserExtended, roleReq, permissionReq *[]string) ([]string, []string, error) {
	roleSet, permissionSet, ownerRoleIDs, ownerPermissionIDs := ownerAPIKeyAccess(owner)

	roleIDs := ownerRoleIDs
	if roleReq != nil {
		roleIDs = cleanAPIKeyIDs(*roleReq)
	}
	for _, id := range roleIDs {
		if _, ok := roleSet[id]; !ok {
			return nil, nil, httputil.NewError("api key role is not assigned to owner", nil, http.StatusForbidden)
		}
	}

	permissionIDs := ownerPermissionIDs
	if permissionReq != nil {
		permissionIDs = cleanAPIKeyIDs(*permissionReq)
	}
	for _, id := range permissionIDs {
		if _, ok := permissionSet[id]; !ok {
			return nil, nil, httputil.NewError("api key permission is not assigned to owner", nil, http.StatusForbidden)
		}
	}

	return roleIDs, permissionIDs, nil
}

func (m *Auth) apiKeyOwner(w http.ResponseWriter, r *http.Request, userID string, requireUserID bool) (*data.UserExtended, bool) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		if requireUserID {
			httputil.HandleError(w, httputil.NewError("user_id is required", nil, http.StatusBadRequest))
			return nil, false
		}

		user := m.xUser(w, r)
		if user == nil {
			return nil, false
		}
		userID = user.ID
	}

	owner, err := m.cache.GetUser(data.GetUserRequest{ID: userID, AddRoles: true, AddPermissions: true})
	if err != nil || owner.Disabled {
		httputil.HandleError(w, httputil.NewError("api key owner not found", err, http.StatusNotFound))
		return nil, false
	}

	return owner, true
}

// CreateAPIKeyAPI creates an api key for the authenticated X-User.
func (m *Auth) CreateAPIKeyAPI(w http.ResponseWriter, r *http.Request) {
	m.createAPIKey(w, r, false)
}

// CreateAPIKeyPrincipalAPI creates an api key for the requested owner.
func (m *Auth) CreateAPIKeyPrincipalAPI(w http.ResponseWriter, r *http.Request) {
	m.createAPIKey(w, r, true)
}

func (m *Auth) createAPIKey(w http.ResponseWriter, r *http.Request, requireUserID bool) {
	cfg := m.cache.Snapshot().APIKey
	if cfg.Disabled {
		httputil.HandleError(w, httputil.NewError("api keys are disabled", nil, http.StatusForbidden))
		return
	}

	var req APIKeyCreateRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}
	owner, ok := m.apiKeyOwner(w, r, req.UserID, requireUserID)
	if !ok {
		return
	}

	var expiresAt *time.Time
	if req.ExpiresIn != "" {
		d, err := str2duration.ParseDuration(req.ExpiresIn)
		if err != nil || d <= 0 {
			httputil.HandleError(w, httputil.NewError("invalid expires_in", err, http.StatusBadRequest))
			return
		}

		t := time.Now().Add(d)
		expiresAt = &t
	}

	// enforce max lifetime from the "api_key" setting
	if maxLifetime := cfg.GetMaxLifetime(); maxLifetime > 0 {
		limit := time.Now().Add(maxLifetime)
		if expiresAt == nil || expiresAt.After(limit) {
			expiresAt = &limit
		}
	}

	roleIDs, permissionIDs, err := apiKeyAccessForOwner(owner, req.RoleIDs, req.PermissionIDs)
	if err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
		return
	}

	key, keyHash, err := generateAPIKey()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot generate api key", err, http.StatusInternalServerError))
		return
	}

	details := req.Details
	if details == nil {
		details = map[string]any{}
	}

	id, err := m.store.CreateAPIKey(r.Context(), APIKeyMeta{
		UserID:        owner.ID,
		Name:          strings.TrimSpace(req.Name),
		RoleIDs:       roleIDs,
		PermissionIDs: permissionIDs,
		Details:       details,
		Disabled:      req.Disabled,
	}, keyHash, expiresAt)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot save api key", err, http.StatusInternalServerError))
		return
	}

	resp := APIKeyCreateResponse{ID: id, Key: key}
	if expiresAt != nil {
		resp.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	}

	httputil.JSON(w, http.StatusOK, Response[APIKeyCreateResponse]{Payload: resp})
}

func apiKeyUpdateFromRequest(owner *data.UserExtended, req APIKeyUpdateRequest) (APIKeyUpdate, error) {
	update := APIKeyUpdate{Name: req.Name, Details: req.Details, Disabled: req.Disabled}
	if req.RoleIDs != nil || req.PermissionIDs != nil {
		roleIDs, permissionIDs, err := apiKeyAccessForOwner(owner, req.RoleIDs, req.PermissionIDs)
		if err != nil {
			return update, err
		}
		if req.RoleIDs != nil {
			update.RoleIDs = &roleIDs
		}
		if req.PermissionIDs != nil {
			update.PermissionIDs = &permissionIDs
		}
	}

	return update, nil
}

// UpdateAPIKeyAPI updates api key principal metadata and access.
func (m *Auth) UpdateAPIKeyAPI(w http.ResponseWriter, r *http.Request) {
	user := m.xUser(w, r)
	if user == nil {
		return
	}
	owner, err := m.cache.GetUser(data.GetUserRequest{ID: user.ID, AddRoles: true, AddPermissions: true})
	if err != nil || owner.Disabled {
		httputil.HandleError(w, httputil.NewError("user not found", err, http.StatusNotFound))
		return
	}

	var req APIKeyUpdateRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	update, err := apiKeyUpdateFromRequest(owner, req)
	if err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
		return
	}

	if err := m.store.UpdateAPIKey(r.Context(), user.ID, r.PathValue("id"), update); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, data.ErrNotFound) {
			code = http.StatusNotFound
		}

		httputil.HandleError(w, httputil.NewError("cannot update api key", err, code))

		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "api key updated"},
	})
}

// UpdateAPIKeyPrincipalAPI updates api key metadata without X-User ownership scoping.
func (m *Auth) UpdateAPIKeyPrincipalAPI(w http.ResponseWriter, r *http.Request) {
	meta, err := m.store.GetAPIKeyMeta(r.Context(), r.PathValue("id"))
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, data.ErrNotFound) {
			code = http.StatusNotFound
		}
		httputil.HandleError(w, httputil.NewError("cannot get api key", err, code))
		return
	}

	owner, ok := m.apiKeyOwner(w, r, meta.UserID, true)
	if !ok {
		return
	}

	var req APIKeyUpdateRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	update, err := apiKeyUpdateFromRequest(owner, req)
	if err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
		return
	}

	if err := m.store.UpdateAPIKeyByID(r.Context(), meta.ID, update); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, data.ErrNotFound) {
			code = http.StatusNotFound
		}
		httputil.HandleError(w, httputil.NewError("cannot update api key", err, code))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "api key updated"},
	})
}

// ListAPIKeysAPI lists api keys of the authenticated X-User.
func (m *Auth) ListAPIKeysAPI(w http.ResponseWriter, r *http.Request) {
	user := m.xUser(w, r)
	if user == nil {
		return
	}

	keys, err := m.store.ListAPIKeys(r.Context(), user.ID)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot list api keys", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[[]APIKeyMeta]{
		Meta:    &Meta{TotalItemCount: uint64(len(keys))},
		Payload: keys,
	})
}

// ListAPIKeyPrincipalsAPI lists api key principals across owners.
func (m *Auth) ListAPIKeyPrincipalsAPI(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)
	ownerID := strings.TrimSpace(q.GetValue("user_id"))

	var (
		keys []APIKeyMeta
		err  error
	)
	if ownerID != "" {
		keys, err = m.store.ListAPIKeys(r.Context(), ownerID)
	} else {
		keys, err = m.store.ListAllAPIKeys(r.Context())
	}
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot list api keys", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[[]APIKeyMeta]{
		Meta:    &Meta{TotalItemCount: uint64(len(keys))},
		Payload: keys,
	})
}

// DeleteAPIKeyAPI deletes an api key owned by the authenticated X-User.
func (m *Auth) DeleteAPIKeyAPI(w http.ResponseWriter, r *http.Request) {
	user := m.xUser(w, r)
	if user == nil {
		return
	}

	if err := m.store.DeleteAPIKey(r.Context(), user.ID, r.PathValue("id")); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, data.ErrNotFound) {
			code = http.StatusNotFound
		}

		httputil.HandleError(w, httputil.NewError("cannot delete api key", err, code))

		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "api key deleted"},
	})
}

// DeleteAPIKeyPrincipalAPI deletes an api key without X-User ownership scoping.
func (m *Auth) DeleteAPIKeyPrincipalAPI(w http.ResponseWriter, r *http.Request) {
	if err := m.store.DeleteAPIKeyByID(r.Context(), r.PathValue("id")); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, data.ErrNotFound) {
			code = http.StatusNotFound
		}
		httputil.HandleError(w, httputil.NewError("cannot delete api key", err, code))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "api key deleted"},
	})
}

// APIKeyAuthAPI validates a raw static api key and returns identity claims
// for its principal. The key comes from the X-API-Key header (or the api_key
// form/query value). This is the remote counterpart of session's in-process
// auth_middleware validation; no JWT is issued.
func (m *Auth) APIKeyAuthAPI(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = r.FormValue("api_key")
	}

	if key == "" {
		httputil.HandleError(w, httputil.NewError("api key is required", nil, http.StatusBadRequest))
		return
	}

	claims, err := m.apiKeyClaimsForKey(r.Context(), key)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("api key not valid", nil, http.StatusUnauthorized))
		return
	}

	w.Header().Set("Cache-Control", "no-store")

	httputil.JSON(w, http.StatusOK, claims)
}
