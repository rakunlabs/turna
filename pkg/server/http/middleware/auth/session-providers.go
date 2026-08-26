package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// UnmarshalJSON keeps group overrides deliberately presentation-only. Without
// strict decoding, misspelled or credential-like fields would be persisted but
// silently ignored by the runtime resolver.
func (o *SessionProviderOverride) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("provider override must be an object")
	}

	type override SessionProviderOverride
	var parsed override
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return err
	}

	*o = SessionProviderOverride(parsed)

	return nil
}

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

func resolveSessionProviderGroup(base map[string]session.Provider, group SessionProviderGroup) map[string]session.Provider {
	providers := make(map[string]session.Provider, len(group.Providers)+len(group.Inherit))
	for name, override := range group.Inherit {
		provider, ok := base[name]
		if !ok {
			continue
		}
		if override.Hide != nil {
			provider.Hide = *override.Hide
		}
		providers[name] = provider
	}
	for name, provider := range group.Providers {
		providers[name] = provider
	}

	return providers
}

// validateSessionProviders enforces the invariants of the session_providers
// namespace on save: usable group names, globally unique full definitions and
// inherited references that resolve to the canonical ungrouped list.
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

		group := setting.Groups[groupName]
		inheritNames := make([]string, 0, len(group.Inherit))
		for name := range group.Inherit {
			inheritNames = append(inheritNames, name)
		}
		sort.Strings(inheritNames)
		for _, name := range inheritNames {
			if _, ok := setting.Providers[name]; !ok {
				return fmt.Errorf("group %q inherits unknown provider key %q", groupName, name)
			}
			if _, ok := group.Providers[name]; ok {
				return fmt.Errorf("provider key %q is both defined and inherited in group %q", name, groupName)
			}
		}
	}

	return nil
}
