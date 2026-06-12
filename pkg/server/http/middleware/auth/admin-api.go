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
	AdminPermission           string `json:"admin_permission"`
	AdminPermissionConfigured bool   `json:"admin_permission_configured"`
	AllowMissingXUser         bool   `json:"allow_missing_x_user"`
	XUser                     string `json:"x_user,omitempty"`
	AuthorizationError        string `json:"authorization_error,omitempty"`
}

func (m *Auth) capabilitiesForRequest(r *http.Request) CapabilitiesResponse {
	cfg := m.cache.Snapshot().Admin
	permission := strings.TrimSpace(cfg.GetPermission())
	xUser := strings.TrimSpace(r.Header.Get("X-User"))

	caps := CapabilitiesResponse{
		AdminPermission:           permission,
		AdminPermissionConfigured: permission != "",
		AllowMissingXUser:         cfg.GetAllowMissingXUser(),
		XUser:                     xUser,
		SelfService:               xUser != "",
	}

	if permission == "" {
		caps.IsAdmin = true
		caps.BootstrapAdmin = true
		caps.AnonymousAdmin = xUser == ""

		return caps
	}

	if xUser == "" {
		if caps.AllowMissingXUser {
			caps.IsAdmin = true
			caps.AnonymousAdmin = true
		} else {
			caps.AuthorizationError = "X-User header is required"
		}

		return caps
	}

	user, err := m.cache.GetUser(data.GetUserRequest{
		Alias:          xUser,
		AddPermissions: true,
	})
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
