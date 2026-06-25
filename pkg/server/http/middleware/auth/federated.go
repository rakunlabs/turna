package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// ClaimMapping maps upstream identity claims (OAuth2/OIDC) or assertion
// attributes (SAML) onto local users, mirroring the LDAP sync model.
type ClaimMapping struct {
	// RolesClaim is the claim/attribute holding group or role values.
	// OAuth2 supports dot paths into nested claims (e.g. "realm_access.roles",
	// "groups"); SAML matches the attribute name or friendly name.
	RolesClaim string `json:"roles_claim"`
	// UseLMap resolves claim values through the LDAP group maps (lmaps),
	// sharing one group->role model across LDAP, OAuth2 and SAML.
	UseLMap bool `json:"use_lmap"`
	// RoleMap maps a claim value directly to role names or role IDs.
	RoleMap map[string][]string `json:"role_map"`
	// Register creates unknown users on first login (non-local, like LDAP).
	Register bool `json:"register"`
}

func (c ClaimMapping) enabled() bool {
	return c.RolesClaim != "" || c.Register
}

// claimValues extracts string values at a dot path from a claims map.
// The target may be a string, a []any of strings or a space separated list.
func claimValues(claims map[string]any, path string) []string {
	if path == "" || claims == nil {
		return nil
	}

	var current any = claims

	for part := range strings.SplitSeq(path, ".") {
		node, ok := current.(map[string]any)
		if !ok {
			return nil
		}

		current = node[part]
	}

	switch v := current.(type) {
	case string:
		return splitFields(v)
	case []any:
		values := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				values = append(values, s)
			}
		}

		return values
	case []string:
		return v
	default:
		return nil
	}
}

// setClaimByPath writes value at a dot path into a claims map, creating the
// intermediate map[string]any nodes as needed. The inverse of claimValues.
// A non-map value blocking the path is overwritten so the leaf is always set.
// An empty path is a no-op.
func setClaimByPath(claims map[string]any, path string, value any) {
	if path == "" || claims == nil {
		return
	}

	parts := strings.Split(path, ".")
	node := claims
	for _, part := range parts[:len(parts)-1] {
		child, ok := node[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			node[part] = child
		}

		node = child
	}

	node[parts[len(parts)-1]] = value
}

// rolesFromClaimValues resolves claim values to role IDs through lmaps
// and the inline role map.
func (m *Auth) rolesFromClaimValues(values []string, mapping ClaimMapping) []string {
	sn := m.cache.Snapshot()

	roleIDs := make([]string, 0, len(values))

	resolveRole := func(nameOrID string) {
		if _, ok := sn.Roles[nameOrID]; ok {
			roleIDs = append(roleIDs, nameOrID)
			return
		}

		if id, ok := sn.RoleNames[nameOrID]; ok {
			roleIDs = append(roleIDs, id)
		}
	}

	for _, value := range values {
		if mapping.UseLMap {
			if lmap, ok := sn.LMaps[value]; ok {
				roleIDs = append(roleIDs, lmap.RoleIDs...)
			}
		}

		for _, role := range mapping.RoleMap[value] {
			resolveRole(role)
		}
	}

	return slicesUnique(roleIDs)
}

// federatedDetails picks profile details from upstream claims.
func federatedDetails(alias string, claims map[string]any) map[string]any {
	details := map[string]any{}

	for _, key := range []string{"email", "name", "given_name", "family_name"} {
		if v, ok := claims[key].(string); ok && v != "" {
			details[key] = v
		}
	}

	if v, ok := claims["preferred_username"].(string); ok && v != "" {
		details["uid"] = v
	}

	if details["email"] == nil && strings.Contains(alias, "@") {
		details["email"] = alias
	}
	if details["name"] == nil {
		details["name"] = alias
	}

	return details
}

// syncFederatedUser creates (when register is enabled) or updates the user
// behind a federated login and syncs the mapped roles into sync_role_ids,
// using the same semantics as the LDAP sync: mapped roles are managed by
// the provider, manually assigned roles stay untouched.
func (m *Auth) syncFederatedUser(ctx context.Context, alias string, claims map[string]any, mapping ClaimMapping) error {
	if !mapping.enabled() {
		return nil
	}

	ctx = data.WithContextUserName(ctx, "FEDERATED")

	var roleIDs []string
	if mapping.RolesClaim != "" {
		roleIDs = m.rolesFromClaimValues(claimValues(claims, mapping.RolesClaim), mapping)
	}

	sn := m.cache.Snapshot()

	user := sn.UserByAlias(alias)
	if user == nil {
		if !mapping.Register {
			return nil
		}

		if _, err := m.store.CreateUser(ctx, data.User{
			Alias:       []string{alias},
			SyncRoleIDs: roleIDs,
			Details:     federatedDetails(alias, claims),
		}); err != nil {
			if errors.Is(err, data.ErrConflict) {
				return nil
			}

			return fmt.Errorf("create federated user: %w", err)
		}

		if err := m.cache.Reload(ctx); err != nil {
			return fmt.Errorf("reload after federated create: %w", err)
		}

		return nil
	}

	// only manage sync roles when a roles claim is configured; this keeps
	// LDAP-synced roles intact for users coming from both directions
	if mapping.RolesClaim == "" || user.Local || user.ServiceAccount {
		return nil
	}

	if data.CompareSlices(user.SyncRoleIDs, roleIDs) {
		return nil
	}

	if err := m.store.UpdateUserSyncRoles(ctx, user.ID, roleIDs); err != nil {
		return fmt.Errorf("update federated user roles: %w", err)
	}

	if err := m.cache.Reload(ctx); err != nil {
		slog.Warn("reload after federated role sync failed", slog.String("error", err.Error()))
	}

	return nil
}
