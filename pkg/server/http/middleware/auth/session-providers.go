package auth

import (
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// SessionProviders implements session.InfSessionProviders: it returns the
// UI-managed session provider list ("session_providers" settings namespace)
// together with the auth cache version. A session middleware configured with
// `provider_source.auth_middleware: <name>` calls this on version change and
// rebuilds its keyfunc/skip paths only when the list actually changed.
func (m *Auth) SessionProviders() (map[string]session.Provider, uint64) {
	snap := m.cache.Snapshot()

	return snap.SessionProviders, snap.Version
}

// SessionProvidersAPI answers GET /v1/session-providers with the UI-managed
// session provider list and the auth version in meta. Remote turna instances
// poll it through `provider_source.url`. Admin-protected: the payload carries
// provider client secrets.
func (m *Auth) SessionProvidersAPI(w http.ResponseWriter, r *http.Request) {
	snap := m.cache.Snapshot()

	httputil.JSON(w, http.StatusOK, Response[map[string]session.Provider]{
		Meta:    &Meta{Version: snap.Version},
		Payload: snap.SessionProviders,
	})
}
