package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

var errAPIKeyOwnerRequired = errors.New("api key owner is required")

// apiKeyUser builds the API key principal as a virtual service-account user.
// A key with an explicit role or permission list carries exactly that list; a
// key with neither acts as its owner and inherits the owner's access live, so
// granting or revoking on the owner applies to the key on the next request.
func (m *Auth) apiKeyUser(meta *APIKeyMeta, owner *data.UserExtended) *data.UserExtended {
	details := make(map[string]any, len(meta.Details)+4)
	for k, v := range meta.Details {
		details[k] = v
	}

	subject := apiKeyPrincipalSubject(meta.ID)
	details["uid"] = subject
	details["api_key_id"] = meta.ID
	if meta.UserID != "" {
		details["owner_user_id"] = meta.UserID
	}
	if _, ok := details["name"]; !ok {
		if meta.Name != "" {
			details["name"] = meta.Name
		} else {
			details["name"] = subject
		}
	}

	user := &data.User{
		ID:             subject,
		Alias:          []string{subject},
		RoleIDs:        meta.RoleIDs,
		PermissionIDs:  meta.PermissionIDs,
		Details:        details,
		Disabled:       meta.Disabled,
		ServiceAccount: true,
	}

	if len(meta.RoleIDs) == 0 && len(meta.PermissionIDs) == 0 && owner != nil && owner.User != nil {
		user.RoleIDs = owner.RoleIDs
		user.SyncRoleIDs = owner.SyncRoleIDs
		user.TmpRoleIDs = owner.TmpRoleIDs
		user.PermissionIDs = owner.PermissionIDs
		user.TmpPermissionIDs = owner.TmpPermissionIDs
	}

	ext := m.cache.Snapshot().extendUser(true, true, false, true, user)
	ext.IsActive = !meta.Disabled

	return &ext
}

func (m *Auth) apiKeyUserByPrincipal(ctx context.Context, principal string) (*data.UserExtended, error) {
	meta, err := m.store.GetAPIKeyPrincipalByID(ctx, principal)
	if err != nil {
		return nil, err
	}

	var owner *data.UserExtended
	if meta.UserID != "" {
		owner, err = m.cache.GetUser(data.GetUserRequest{ID: meta.UserID})
		if err != nil || owner.Disabled {
			return nil, fmt.Errorf("api key owner not found; %w", data.ErrNotFound)
		}
	}

	return m.apiKeyUser(meta, owner), nil
}

// userForPrincipal resolves user-scoped operations to the owner of an API key
// while leaving authorization checks on the key's own virtual principal.
func (m *Auth) userForPrincipal(ctx context.Context, principal string, req data.GetUserRequest) (*data.UserExtended, error) {
	if strings.HasPrefix(principal, apiKeyPrincipalPrefix) {
		meta, err := m.store.GetAPIKeyPrincipalByID(ctx, principal)
		if err != nil {
			return nil, err
		}
		if meta.UserID == "" {
			return nil, errAPIKeyOwnerRequired
		}

		req.Alias = ""
		req.ID = meta.UserID
	} else {
		req.ID = ""
		req.Alias = principal
	}

	user, err := m.cache.GetUser(req)
	if err != nil || user.Disabled {
		return nil, fmt.Errorf("user not found; %w", data.ErrNotFound)
	}

	return user, nil
}

// apiKeyClaimsForKey validates a raw static api key against the database and
// returns claim-shaped identity data for it. Every call sees the current key
// state, so deleted/disabled/expired keys fail immediately.
func (m *Auth) apiKeyClaimsForKey(ctx context.Context, key string) (map[string]any, error) {
	if m.cache.Snapshot().APIKey.Disabled {
		return nil, fmt.Errorf("api keys are disabled; %w", data.ErrInvalidRequest)
	}

	meta, err := m.store.GetAPIKeyPrincipal(ctx, key)
	if err != nil {
		return nil, err
	}

	// system keys have no owner; owned keys die with a missing/disabled owner
	var owner *data.UserExtended
	if meta.UserID != "" {
		owner, err = m.cache.GetUser(data.GetUserRequest{ID: meta.UserID})
		if err != nil || owner.Disabled {
			return nil, fmt.Errorf("api key owner not found; %w", data.ErrNotFound)
		}
	}

	return m.apiKeyClaims(meta, owner), nil
}

func (m *Auth) apiKeyClaims(meta *APIKeyMeta, owner *data.UserExtended) map[string]any {
	user := m.apiKeyUser(meta, owner)
	subject := apiKeyPrincipalSubject(meta.ID)
	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = strings.TrimSpace(castString(user.Details["name"]))
	}
	if name == "" {
		name = subject
	}

	email := strings.TrimSpace(castString(meta.Details["email"]))
	if email == "" && owner != nil && owner.User != nil {
		email = strings.TrimSpace(castString(owner.Details["email"]))
	}

	claims := map[string]any{
		"sub":                subject,
		"preferred_username": name,
		"name":               name,
		"typ":                "APIKey",
		"principal_type":     "api_key",
		"api_key_id":         meta.ID,
	}
	if meta.UserID != "" {
		claims["owner_user_id"] = meta.UserID
	}
	if email != "" {
		claims["email"] = email
	}

	roles := idNameClaimValues(user.Roles)
	if len(roles) > 0 {
		claims["roles"] = roles
	}

	permissions := idNameClaimValues(user.Permissions)
	if len(permissions) > 0 {
		claims["permissions"] = permissions
	}

	return claims
}

func idNameClaimValues(values []data.IDName) []string {
	items := make([]string, 0, len(values)*2)
	for _, item := range values {
		if item.ID != "" {
			items = append(items, item.ID)
		}
		if item.Name != "" {
			items = append(items, item.Name)
		}
	}

	return slicesUnique(items)
}
