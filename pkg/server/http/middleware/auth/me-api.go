package auth

import (
	"fmt"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// MeResponse is the self-service account overview for the X-User identity.
type MeResponse struct {
	ID          string         `json:"id"`
	Alias       []string       `json:"alias"`
	Details     map[string]any `json:"details"`
	Roles       []string       `json:"roles"`
	Permissions []string       `json:"permissions"`
	IsActive    bool           `json:"is_active"`
	// Local users manage their password here; non-local users authenticate
	// against LDAP or an upstream provider.
	Local bool `json:"local"`

	// security material overview
	TOTPEnabled  bool `json:"totp_enabled"`
	PasskeyCount int  `json:"passkey_count"`
	APIKeyCount  int  `json:"api_key_count"`
}

// MeAPI returns the authenticated user's profile, roles, permissions and
// security material overview.
func (m *Auth) MeAPI(w http.ResponseWriter, r *http.Request) {
	alias := r.Header.Get("X-User")
	if alias == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusUnauthorized))
		return
	}

	user, err := m.cache.GetUser(data.GetUserRequest{
		Alias:          alias,
		AddRoles:       true,
		AddPermissions: true,
		Sanitize:       true,
	})
	if err != nil {
		httputil.HandleError(w, httputil.NewError("user not found", err, http.StatusNotFound))
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

	resp := MeResponse{
		ID:          user.ID,
		Alias:       user.Alias,
		Details:     user.Details,
		Roles:       roles,
		Permissions: permissions,
		IsActive:    user.IsActive,
		Local:       user.Local,
	}

	if _, confirmed, err := m.store.GetTOTPSecret(r.Context(), user.ID); err == nil {
		resp.TOTPEnabled = confirmed
	}

	if creds, err := m.store.ListPasskeyCredentials(r.Context(), user.ID); err == nil {
		resp.PasskeyCount = len(creds)
	}

	if keys, err := m.store.ListAPIKeys(r.Context(), user.ID); err == nil {
		resp.APIKeyCount = len(keys)
	}

	httputil.JSON(w, http.StatusOK, Response[MeResponse]{Payload: resp})
}

type MePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

const minPasswordLength = 8

// MePasswordAPI changes the password of the authenticated local user.
// The current password must be provided and verified.
func (m *Auth) MePasswordAPI(w http.ResponseWriter, r *http.Request) {
	alias := r.Header.Get("X-User")
	if alias == "" {
		httputil.HandleError(w, httputil.NewError("X-User header is required", nil, http.StatusUnauthorized))
		return
	}

	// non-sanitized read; the stored bcrypt hash is needed for verification
	user, err := m.cache.GetUser(data.GetUserRequest{Alias: alias})
	if err != nil {
		httputil.HandleError(w, httputil.NewError("user not found", err, http.StatusNotFound))
		return
	}

	if !user.Local {
		httputil.HandleError(w, httputil.NewError("password is managed by an external identity provider", nil, http.StatusForbidden))
		return
	}

	var req MePasswordRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	if minLen := m.cache.Snapshot().Signup.GetPasswordMinLength(); len(req.NewPassword) < minLen {
		httputil.HandleError(w, httputil.NewError(fmt.Sprintf("new password must be at least %d characters", minLen), nil, http.StatusBadRequest))
		return
	}

	current, _ := user.Details["password"].(string)
	if current == "" || compareBcryptBase64(current, req.CurrentPassword) != nil {
		httputil.HandleError(w, httputil.NewError("current password not match", nil, http.StatusUnauthorized))
		return
	}

	if err := m.store.UpdateUserPassword(userCtx(r), user.ID, req.NewPassword); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot update password", err, http.StatusInternalServerError))
		return
	}

	// apply immediately on this instance; others converge via polling
	if err := m.cache.Reload(r.Context()); err != nil {
		httputil.HandleError(w, httputil.NewError("password updated but reload failed", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "password updated"},
	})
}
