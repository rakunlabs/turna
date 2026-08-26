package auth

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// SessionProviders implements session.InfSessionProviders: it returns the
// UI-managed session provider list ("session_providers" settings namespace,
// ungrouped entries plus every group merged in) together with the auth cache
// version. A session middleware configured with
// `provider_source.auth_middleware: <name>` calls this on version change and
// rebuilds its keyfunc/skip paths only when the list actually changed.
func (m *Auth) SessionProviders() (map[string]session.Provider, uint64) {
	snap := m.cache.Snapshot()

	return snap.SessionProviders, snap.Version
}

// SessionProvidersGroup implements session.InfSessionProviderGroups: it
// returns the providers of one named group of the "session_providers"
// namespace. found is false when the group does not exist.
func (m *Auth) SessionProvidersGroup(group string) (map[string]session.Provider, uint64, bool) {
	snap := m.cache.Snapshot()

	providers, ok := snap.SessionProviderGroups[group]

	return providers, snap.Version, ok
}

// SessionProviderCatalog implements session.InfSessionProviderCatalog. Both
// maps come from the same immutable auth snapshot, allowing a session to keep
// all groups available for login presentation while validating with only its
// configured provider_source.group.
func (m *Auth) SessionProviderCatalog() (map[string]session.Provider, map[string]map[string]session.Provider, uint64) {
	snap := m.cache.Snapshot()

	return snap.SessionProviders, snap.SessionProviderGroups, snap.Version
}

// SessionProvidersAPI answers GET /v1/session-providers with the UI-managed
// session provider list (ungrouped plus all groups merged) and the auth
// version in meta. Remote turna instances poll it through
// `provider_source.url`. Admin-protected: the payload carries provider
// client secrets.
func (m *Auth) SessionProvidersAPI(w http.ResponseWriter, r *http.Request) {
	snap := m.cache.Snapshot()

	httputil.JSON(w, http.StatusOK, Response[map[string]session.Provider]{
		Meta:    &Meta{Version: snap.Version},
		Payload: snap.SessionProviders,
	})
}

// SessionProvidersGroupAPI answers GET /v1/session-providers/{group} with
// the providers of one named group, so different session middleware
// instances can pull different subsets from the same auth. Same envelope
// and protection as the merged endpoint; 404 when the group is unknown.
func (m *Auth) SessionProvidersGroupAPI(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")

	providers, version, ok := m.SessionProvidersGroup(group)
	if !ok {
		httputil.HandleError(w, httputil.NewError("session provider group not found", nil, http.StatusNotFound))

		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]session.Provider]{
		Meta:    &Meta{Version: version},
		Payload: providers,
	})
}

// validateSessionProviders enforces the invariants of the session_providers
// namespace on save: usable group names (they travel as a path segment) and
// globally unique provider keys, so the merged view and the login page never
// see two definitions of the same provider.
func validateSessionProviders(setting SessionProviderSettings) error {
	seen := make(map[string]string, len(setting.Providers))
	for name := range setting.Providers {
		seen[name] = "the ungrouped list"
	}

	groupNames := make([]string, 0, len(setting.Groups))
	for groupName := range setting.Groups {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)

	for _, groupName := range groupNames {
		if strings.TrimSpace(groupName) == "" {
			return fmt.Errorf("group name cannot be empty")
		}
		if strings.ContainsAny(groupName, "/?#% ") {
			return fmt.Errorf("group name %q cannot contain '/', '?', '#', '%%' or spaces", groupName)
		}

		providerNames := make([]string, 0, len(setting.Groups[groupName].Providers))
		for name := range setting.Groups[groupName].Providers {
			providerNames = append(providerNames, name)
		}
		sort.Strings(providerNames)

		for _, name := range providerNames {
			if where, ok := seen[name]; ok {
				return fmt.Errorf("provider key %q appears in %s and in group %q; provider keys must be unique across groups", name, where, groupName)
			}

			seen[name] = fmt.Sprintf("group %q", groupName)
		}
	}

	return nil
}
