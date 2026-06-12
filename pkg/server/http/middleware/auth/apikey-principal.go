package auth

import (
	"context"
	"fmt"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// apiKeyUser builds the API key principal as a virtual service-account user;
// roles and permissions come from the key itself, not from its owner.
func (m *Auth) apiKeyUser(meta *APIKeyMeta) *data.UserExtended {
	details := make(map[string]any, len(meta.Details)+4)
	for k, v := range meta.Details {
		details[k] = v
	}

	subject := apiKeyPrincipalSubject(meta.ID)
	details["uid"] = subject
	details["api_key_id"] = meta.ID
	details["owner_user_id"] = meta.UserID
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

	ext := m.cache.Snapshot().extendUser(true, true, false, true, user)
	ext.IsActive = !meta.Disabled

	return &ext
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

	owner, err := m.cache.GetUser(data.GetUserRequest{ID: meta.UserID})
	if err != nil || owner.Disabled {
		return nil, fmt.Errorf("api key owner not found; %w", data.ErrNotFound)
	}

	user := m.apiKeyUser(meta)
	subject := apiKeyPrincipalSubject(meta.ID)

	claims := map[string]any{
		"sub":                subject,
		"preferred_username": subject,
		"name":               user.Details["name"],
		"typ":                "APIKey",
		"principal_type":     "api_key",
		"api_key_id":         meta.ID,
		"owner_user_id":      meta.UserID,
	}

	roles := idNameClaimValues(user.Roles)
	if len(roles) > 0 {
		claims["roles"] = roles
	}

	permissions := idNameClaimValues(user.Permissions)
	if len(permissions) > 0 {
		claims["permissions"] = permissions
	}

	return claims, nil
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
