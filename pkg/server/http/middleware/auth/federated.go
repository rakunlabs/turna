package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
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
	// LinkLocalUsers lets a first federated login attach to an existing local
	// (password) account with the same alias. Off by default: otherwise anyone
	// controlling an upstream account with that email or username would log
	// in as the local user. Service accounts are never attached.
	LinkLocalUsers bool `json:"link_local_users"`
	// RequireEmailVerified refuses logins whose alias comes from the email
	// claim unless the provider reports email_verified=true. OAuth2/OIDC only;
	// SAML assertions carry no such claim.
	RequireEmailVerified bool `json:"require_email_verified"`
	// SubjectClaim names the claim (OAuth2) or attribute (SAML) holding the
	// provider's stable account id used to link logins to a user. Default:
	// "sub" for OAuth2, a persistent NameID for SAML. "-" disables linking.
	SubjectClaim string `json:"subject_claim"`
}

// samlPersistentNameID is the only NameID format guaranteed stable per user.
const samlPersistentNameID = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"

// federatedSubject picks the stable upstream account id from the claims.
func federatedSubject(claims map[string]any, mapping ClaimMapping, defaultClaim string) string {
	key := mapping.SubjectClaim
	if key == "-" {
		return ""
	}
	if key == "" {
		key = defaultClaim
	}

	switch v := claims[key].(type) {
	case string:
		return v
	case []any:
		if len(v) > 0 {
			s, _ := v[0].(string)
			return s
		}
	case float64:
		// numeric ids (e.g. GitHub's "id")
		return strconv.FormatFloat(v, 'f', -1, 64)
	}

	return ""
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

// errFederatedRefused marks a federated login refused by the linking policy
// (as opposed to an internal failure).
var errFederatedRefused = errors.New("federated login refused")

// federatedLogin is the upstream identity behind a federated login.
type federatedLogin struct {
	// Provider is the link namespace, e.g. "oauth2:google" or "saml:corp".
	Provider string
	// Subject is the provider's stable account id (OIDC sub, persistent SAML
	// NameID); empty when the provider offers none, which disables linking.
	Subject string
	Alias   string
	Claims  map[string]any
	Mapping ClaimMapping
}

// resolveFederatedUser returns the local user a federated login signs in as.
//
//  1. An existing (provider, subject) link wins, even if the upstream email
//     or username changed since.
//  2. Otherwise the alias is matched, never against a service account and
//     against a local account only with link_local_users; a user already
//     linked to another account of this provider is refused.
//  3. Otherwise the user is created (claim_mapping.register) or looked up
//     through LDAP.
//
// The first successful match is linked so later logins take step 1. Mapped
// roles are synced into sync_role_ids as before.
func (m *Auth) resolveFederatedUser(ctx context.Context, login federatedLogin) (*data.User, error) {
	ctx = data.WithContextUserName(ctx, "FEDERATED")

	user, err := m.federatedUser(ctx, login)
	if err != nil {
		return nil, err
	}

	if login.Subject != "" {
		if err := m.store.LinkFederatedIdentity(ctx, login.Provider, login.Subject, user.ID, login.Alias); err != nil {
			if errors.Is(err, data.ErrConflict) {
				return nil, fmt.Errorf("%w: %w", errFederatedRefused, err)
			}

			return nil, fmt.Errorf("link federated identity: %w", err)
		}
	}

	if err := m.syncFederatedRoles(ctx, user, login); err != nil {
		return nil, err
	}

	return user, nil
}

func (m *Auth) federatedUser(ctx context.Context, login federatedLogin) (*data.User, error) {
	if login.Subject != "" {
		link, err := m.store.GetFederatedIdentity(ctx, login.Provider, login.Subject)
		switch {
		case err == nil:
			if user := m.cache.Snapshot().Users[link.UserID]; user != nil {
				return user, nil
			}
			// linked user is gone from the cache (deleted meanwhile); the row
			// cascades away with it, so fall through to a fresh resolution
		case !errors.Is(err, data.ErrNotFound):
			return nil, fmt.Errorf("get federated identity: %w", err)
		}
	}

	if login.Mapping.RequireEmailVerified && claimString(login.Claims, "email") == login.Alias && !claimTrue(login.Claims["email_verified"]) {
		return nil, fmt.Errorf("%w: email %q is not verified by the provider", errFederatedRefused, login.Alias)
	}

	if user := m.cache.Snapshot().UserByAlias(login.Alias); user != nil {
		return federatedAttachable(user, login.Mapping)
	}

	if login.Mapping.Register {
		roleIDs := m.federatedRoleIDs(login)
		if _, err := m.store.CreateUser(ctx, data.User{
			Alias:       []string{login.Alias},
			SyncRoleIDs: roleIDs,
			Details:     federatedDetails(login.Alias, login.Claims),
		}); err != nil && !errors.Is(err, data.ErrConflict) {
			// a conflict means a concurrent first login created it; resolve below
			return nil, fmt.Errorf("create federated user: %w", err)
		}
	} else if m.ldapEnabled() {
		if err := m.LdapSync(ctx, false, login.Alias); err != nil {
			slog.Debug("ldap lookup for federated login failed", slog.String("alias", login.Alias), slog.String("error", err.Error()))
		}
	}

	if err := m.cache.Reload(ctx); err != nil {
		return nil, fmt.Errorf("reload after federated create: %w", err)
	}

	if user := m.cache.Snapshot().UserByAlias(login.Alias); user != nil {
		return federatedAttachable(user, login.Mapping)
	}

	return nil, fmt.Errorf("%w: user %q not found", errFederatedRefused, login.Alias)
}

// federatedAttachable applies the alias-matching policy.
func federatedAttachable(user *data.User, mapping ClaimMapping) (*data.User, error) {
	if user.ServiceAccount {
		return nil, fmt.Errorf("%w: alias belongs to a service account", errFederatedRefused)
	}

	if user.Local && !mapping.LinkLocalUsers {
		return nil, fmt.Errorf("%w: alias belongs to a local account (enable claim_mapping.link_local_users to allow)", errFederatedRefused)
	}

	return user, nil
}

func (m *Auth) federatedRoleIDs(login federatedLogin) []string {
	if login.Mapping.RolesClaim == "" {
		return nil
	}

	return m.rolesFromClaimValues(claimValues(login.Claims, login.Mapping.RolesClaim), login.Mapping)
}

// syncFederatedRoles syncs the mapped roles into sync_role_ids with the LDAP
// semantics: mapped roles are managed by the provider, manually assigned
// roles stay untouched. Only when a roles claim is configured, which keeps
// LDAP-synced roles intact for users coming from both directions.
func (m *Auth) syncFederatedRoles(ctx context.Context, user *data.User, login federatedLogin) error {
	if login.Mapping.RolesClaim == "" || user.Local || user.ServiceAccount {
		return nil
	}

	roleIDs := m.federatedRoleIDs(login)
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

func claimString(claims map[string]any, key string) string {
	v, _ := claims[key].(string)

	return v
}

// claimTrue accepts a boolean true or the string "true" (some providers
// serialize email_verified as a string).
func claimTrue(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true")
	default:
		return false
	}
}
