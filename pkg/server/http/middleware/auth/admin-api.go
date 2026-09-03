package auth

import (
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

type CapabilitiesResponse struct {
	IsAdmin                   bool   `json:"is_admin"`
	AnonymousAdmin            bool   `json:"anonymous_admin"`
	BootstrapAdmin            bool   `json:"bootstrap_admin"`
	SelfService               bool   `json:"self_service"`
	APIKeySelfService         bool   `json:"api_key_self_service"`
	AdminPermission           string `json:"admin_permission"`
	AdminPermissionConfigured bool   `json:"admin_permission_configured"`
	AllowMissingXUser         bool   `json:"allow_missing_x_user"`
	XUser                     string `json:"x_user,omitempty"`
	AuthorizationError        string `json:"authorization_error,omitempty"`
}

func (m *Auth) capabilitiesForRequest(r *http.Request) CapabilitiesResponse {
	sn := m.cache.Snapshot()
	cfg := sn.Admin
	permission := strings.TrimSpace(cfg.GetPermission())
	xUser := strings.TrimSpace(r.Header.Get("X-User"))
	principal := requestPrincipal(r)

	caps := CapabilitiesResponse{
		AdminPermission:           permission,
		AdminPermissionConfigured: permission != "",
		AllowMissingXUser:         cfg.GetAllowMissingXUser(),
		XUser:                     xUser,
		SelfService:               principal != "",
		APIKeySelfService:         principal != "" && sn.APIKey.SelfService && !sn.APIKey.Disabled,
	}

	if permission == "" {
		caps.IsAdmin = true
		caps.BootstrapAdmin = true
		caps.AnonymousAdmin = principal == ""

		return caps
	}

	if principal == "" {
		if caps.AllowMissingXUser {
			caps.IsAdmin = true
			caps.AnonymousAdmin = true
		} else {
			caps.AuthorizationError = "X-User header is required"
		}

		return caps
	}

	var user *data.UserExtended
	var err error
	if strings.HasPrefix(principal, apiKeyPrincipalPrefix) {
		user, err = m.apiKeyUserByPrincipal(r.Context(), principal)
	} else {
		user, err = m.cache.GetUser(data.GetUserRequest{
			Alias:          principal,
			AddPermissions: true,
		})
	}
	if err != nil || user.Disabled {
		caps.AuthorizationError = "user not found"
		return caps
	}

	for _, item := range user.Permissions {
		if item.ID == permission || item.Name == permission {
			caps.IsAdmin = true
			return caps
		}
	}

	caps.AuthorizationError = "admin permission is required"

	return caps
}

func (m *Auth) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	caps := m.capabilitiesForRequest(r)
	if caps.IsAdmin {
		return true
	}

	code := http.StatusForbidden
	if caps.XUser == "" {
		code = http.StatusUnauthorized
	}
	message := caps.AuthorizationError
	if message == "" {
		message = "admin permission is required"
	}

	httputil.HandleError(w, httputil.NewError(message, nil, code))

	return false
}

func (m *Auth) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.requireAdmin(w, r) {
			return
		}

		next(w, r)
	}
}

// apiKeySelfOrAdmin admits the /v1/api-keys plane: any authenticated X-User
// when the api_key.self_service setting is on (the handlers scope every read
// and write to that X-User), and admins always. With self-service off the
// plane behaves exactly like before — admin only.
func (m *Auth) apiKeySelfOrAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := m.cache.Snapshot().APIKey
		if cfg.SelfService && !cfg.Disabled && requestPrincipal(r) != "" {
			next(w, r)

			return
		}

		if !m.requireAdmin(w, r) {
			return
		}

		next(w, r)
	}
}

func (m *Auth) adminOnlyHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.requireAdmin(w, r) {
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (m *Auth) CapabilitiesAPI(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, Response[CapabilitiesResponse]{Payload: m.capabilitiesForRequest(r)})
}
