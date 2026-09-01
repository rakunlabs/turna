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
	// UserID owns the key. On the self-service plane it may only be the
	// caller's own id; on the admin plane an empty value issues a standalone
	// system key that carries its own roles and permissions.
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

func (m *Auth) xUserRequest(w http.ResponseWriter, r *http.Request, req data.GetUserRequest) *data.UserExtended {
	principal := r.Header.Get("X-User")
	if principal == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusUnauthorized))
		return nil
	}

	user, err := m.userForPrincipal(r.Context(), principal, req)
	if err != nil {
		if errors.Is(err, errAPIKeyOwnerRequired) {
			httputil.HandleError(w, httputil.NewError("api key has no owner", err, http.StatusForbidden))
			return nil
		}

		httputil.HandleError(w, httputil.NewError("user not found", err, http.StatusNotFound))
		return nil
	}

	return user
}

// xUser resolves the X-User principal to the effective user for owner-scoped
// operations, writing the error response itself when resolution fails.
func (m *Auth) xUser(w http.ResponseWriter, r *http.Request) *data.UserExtended {
	return m.xUserRequest(w, r, data.GetUserRequest{})
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

func ownerAPIKeyAccess(owner *data.UserExtended) (map[string]struct{}, map[string]struct{}) {
	roleSet := make(map[string]struct{}, len(owner.Roles))
	permissionSet := make(map[string]struct{}, len(owner.Permissions))

	for _, role := range owner.Roles {
		roleSet[role.ID] = struct{}{}
	}
	for _, permission := range owner.Permissions {
		permissionSet[permission.ID] = struct{}{}
	}

	return roleSet, permissionSet
}

// apiKeyAccessForOwner validates a requested access list against the owner's
// expanded access. An explicit entry the owner does not carry is refused; an
// empty (or absent) list stays empty on the stored key, which means the key
// acts as its owner and inherits the owner's access live at validation time.
func apiKeyAccessForOwner(owner *data.UserExtended, roleReq, permissionReq *[]string) ([]string, []string, error) {
	roleSet, permissionSet := ownerAPIKeyAccess(owner)

	roleIDs := []string{}
	if roleReq != nil {
		roleIDs = cleanAPIKeyIDs(*roleReq)
	}
	for _, id := range roleIDs {
		if _, ok := roleSet[id]; !ok {
			return nil, nil, httputil.NewError("api key role is not assigned to owner", nil, http.StatusForbidden)
		}
	}

	permissionIDs := []string{}
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

// apiKeyRegistryAccess validates the access of an ownerless system key
// against the registry itself: every id must exist. The owner ceiling does
// not apply — only administrators reach this path.
func (m *Auth) apiKeyRegistryAccess(roleReq, permissionReq *[]string) ([]string, []string, error) {
	sn := m.cache.Snapshot()

	roleIDs := []string{}
	if roleReq != nil {
		roleIDs = cleanAPIKeyIDs(*roleReq)
	}
	for _, id := range roleIDs {
		if _, ok := sn.Roles[id]; !ok {
			return nil, nil, httputil.NewError("api key role not found: "+id, nil, http.StatusBadRequest)
		}
	}

	permissionIDs := []string{}
	if permissionReq != nil {
		permissionIDs = cleanAPIKeyIDs(*permissionReq)
	}
	for _, id := range permissionIDs {
		if _, ok := sn.Permissions[id]; !ok {
			return nil, nil, httputil.NewError("api key permission not found: "+id, nil, http.StatusBadRequest)
		}
	}

	return roleIDs, permissionIDs, nil
}

// errSystemKeyNeedsAccess refuses a system key that would end up with no
// access at all — without an owner to inherit from it could never be used.
func errSystemKeyNeedsAccess() error {
	return httputil.NewError("a system key needs at least one role or permission", nil, http.StatusBadRequest)
}

func (m *Auth) apiKeyOwnerByID(w http.ResponseWriter, userID string) (*data.UserExtended, bool) {
	owner, err := m.cache.GetUser(data.GetUserRequest{ID: userID, AddRoles: true, AddPermissions: true})
	if err != nil || owner.Disabled {
		httputil.HandleError(w, httputil.NewError("api key owner not found", err, http.StatusNotFound))
		return nil, false
	}

	return owner, true
}

// CreateAPIKeyAPI creates an api key for the authenticated X-User. The owner
// is always the caller: a user_id naming someone else is refused, so the
// self-service plane can never mint a key that acts as another principal.
func (m *Auth) CreateAPIKeyAPI(w http.ResponseWriter, r *http.Request) {
	m.createAPIKey(w, r, false)
}

// CreateAPIKeyPrincipalAPI creates an api key on the admin plane: with a
// user_id the key belongs to that principal and is capped by their access,
// without one it is a standalone system key carrying its own explicit roles
// and permissions.
func (m *Auth) CreateAPIKeyPrincipalAPI(w http.ResponseWriter, r *http.Request) {
	m.createAPIKey(w, r, true)
}

func (m *Auth) createAPIKey(w http.ResponseWriter, r *http.Request, adminPlane bool) {
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

	reqUserID := strings.TrimSpace(req.UserID)

	var owner *data.UserExtended
	switch {
	case !adminPlane:
		// self-service: the caller is the owner, whatever the body says.
		caller := m.xUser(w, r)
		if caller == nil {
			return
		}
		if reqUserID != "" && reqUserID != caller.ID {
			httputil.HandleError(w, httputil.NewError("cannot issue an api key for another user", nil, http.StatusForbidden))
			return
		}

		var ok bool
		if owner, ok = m.apiKeyOwnerByID(w, caller.ID); !ok {
			return
		}
	case reqUserID != "":
		var ok bool
		if owner, ok = m.apiKeyOwnerByID(w, reqUserID); !ok {
			return
		}
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

	var (
		roleIDs, permissionIDs []string
		err                    error
	)
	if owner != nil {
		roleIDs, permissionIDs, err = apiKeyAccessForOwner(owner, req.RoleIDs, req.PermissionIDs)
	} else {
		roleIDs, permissionIDs, err = m.apiKeyRegistryAccess(req.RoleIDs, req.PermissionIDs)
		if err == nil && len(roleIDs) == 0 && len(permissionIDs) == 0 {
			err = errSystemKeyNeedsAccess()
		}
	}
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

	ownerID := ""
	if owner != nil {
		ownerID = owner.ID
	}

	id, err := m.store.CreateAPIKey(r.Context(), APIKeyMeta{
		UserID:        ownerID,
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

// UpdateAPIKeyPrincipalAPI updates api key metadata without X-User ownership
// scoping. Owned keys keep their owner as the access ceiling; system keys
// (no owner) validate their access against the registry instead.
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

	var req APIKeyUpdateRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	var update APIKeyUpdate
	if meta.UserID != "" {
		owner, ok := m.apiKeyOwnerByID(w, meta.UserID)
		if !ok {
			return
		}

		update, err = apiKeyUpdateFromRequest(owner, req)
	} else {
		update = APIKeyUpdate{Name: req.Name, Details: req.Details, Disabled: req.Disabled}
		if req.RoleIDs != nil || req.PermissionIDs != nil {
			var roleIDs, permissionIDs []string
			roleIDs, permissionIDs, err = m.apiKeyRegistryAccess(req.RoleIDs, req.PermissionIDs)
			if err == nil {
				// an untouched field keeps its stored value; the result must
				// still leave the key with some access.
				finalRoles, finalPermissions := meta.RoleIDs, meta.PermissionIDs
				if req.RoleIDs != nil {
					update.RoleIDs = &roleIDs
					finalRoles = roleIDs
				}
				if req.PermissionIDs != nil {
					update.PermissionIDs = &permissionIDs
					finalPermissions = permissionIDs
				}
				if len(finalRoles) == 0 && len(finalPermissions) == 0 {
					err = errSystemKeyNeedsAccess()
				}
			}
		}
	}
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
