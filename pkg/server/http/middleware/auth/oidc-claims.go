package auth

import "github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"

// standardScopeClaims maps the OIDC Core §5.4 scopes to the claims they
// release, limited to what a user record can carry.
var standardScopeClaims = map[string][]string{
	"profile": {
		"name", "given_name", "family_name", "middle_name", "nickname", "preferred_username",
		"profile", "picture", "website", "gender", "birthdate", "zoneinfo", "locale", "updated_at",
	},
	"email":   {"email", "email_verified"},
	"phone":   {"phone_number", "phone_number_verified"},
	"address": {"address"},
}

// standardScopes are the scopes advertised in discovery next to openid.
var standardScopes = []string{"profile", "email", "phone", "address"}

// userStandardClaims returns the user's standard claims. With strict set
// only the claims released by scopes are included; otherwise the historic
// set (name, preferred_username, email, given_name, family_name) is always
// returned.
func userStandardClaims(user *data.UserExtended, scopes map[string]struct{}, strict bool) map[string]any {
	all := map[string]any{
		"name":               user.Details["name"],
		"preferred_username": preferredUsername(user),
	}
	for _, claims := range standardScopeClaims {
		for _, k := range claims {
			if _, set := all[k]; set {
				continue
			}
			if v, ok := user.Details[k]; ok {
				all[k] = v
			}
		}
	}

	out := make(map[string]any, len(all))
	if !strict {
		for _, k := range []string{"name", "preferred_username", "email", "given_name", "family_name"} {
			if v, ok := all[k]; ok {
				out[k] = v
			}
		}

		return out
	}

	for scope, claims := range standardScopeClaims {
		if _, ok := scopes[scope]; !ok {
			continue
		}

		for _, k := range claims {
			if v, ok := all[k]; ok && v != nil && v != "" {
				out[k] = v
			}
		}
	}

	return out
}

// preferredUsername is the uid (LDAP), else the primary alias, else the
// display name. It must name the user to the session middleware (X-User), so
// an alias beats the display name, which is not unique and not a login.
func preferredUsername(user *data.UserExtended) any {
	if v, ok := user.Details["uid"]; ok && v != "" {
		return v
	}
	if len(user.Alias) > 0 && user.Alias[0] != "" {
		return user.Alias[0]
	}

	return user.Details["name"]
}
