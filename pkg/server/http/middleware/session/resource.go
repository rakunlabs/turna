package session

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// wellKnownProtectedResource is the RFC 9728 protected resource metadata
// path (path-insertion style).
const wellKnownProtectedResource = "/.well-known/oauth-protected-resource"

// InfIssuerURL is implemented by issuers that know their canonical external
// issuer URL (the auth middleware derives it from oauth2.base_url or the
// forwarded headers of the request). The session middleware uses it to fill
// authorization_servers of the protected resource metadata without extra
// configuration.
type InfIssuerURL interface {
	IssuerURL(r *http.Request) string
}

// ProtectedResource publishes this session-protected surface as an OAuth2
// protected resource (RFC 9728): the metadata document is served under
// /.well-known/oauth-protected-resource and 401 challenges carry a
// resource_metadata pointer so discovery-driven clients (MCP) find the
// authorization server behind the login redirect.
type ProtectedResource struct {
	// Resource is the canonical RFC 8707/9728 resource identifier
	// (e.g. https://example.com). Empty derives {scheme}://{host} from each
	// request honoring X-Forwarded-Proto/Host.
	Resource string `cfg:"resource"`

	// AuthorizationServers overrides the issuer URLs listed in the
	// metadata. Empty derives them from providers backed by an in-process
	// auth middleware (auth_middleware).
	AuthorizationServers []string `cfg:"authorization_servers"`

	// ScopesSupported advertised in the metadata document.
	ScopesSupported []string `cfg:"scopes_supported"`
}

// requestBase returns {scheme}://{host} honoring forwarded headers.
func requestBase(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	host := r.Header.Get("X-Forwarded-Host")

	if host == "" {
		host = r.Host
	}
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	return scheme + "://" + host
}

// resourceID returns the canonical resource identifier for this request.
func (m *Session) resourceID(r *http.Request) string {
	if m.ProtectedResource != nil && m.ProtectedResource.Resource != "" {
		return m.ProtectedResource.Resource
	}

	return requestBase(r)
}

// resourceMetadataURL builds the RFC 9728 metadata URL for the resource by
// path-insertion on the resource identifier (RFC 9728 §3.1).
func (m *Session) resourceMetadataURL(r *http.Request) string {
	resource := m.resourceID(r)

	rest := resource
	if idx := strings.Index(resource, "://"); idx >= 0 {
		rest = resource[idx+3:]
	}

	path := ""
	if idx := strings.Index(rest, "/"); idx >= 0 {
		path = rest[idx:]
	}

	origin := strings.TrimSuffix(resource, path)

	return origin + wellKnownProtectedResource + strings.TrimSuffix(path, "/")
}

// resourceAuthorizationServers returns the issuer URLs for the metadata:
// the configured authorization_servers when set, otherwise the issuer URLs
// of every provider backed by an in-process auth middleware that implements
// InfIssuerURL. Providers are visited by priority (then name) so the order
// is deterministic.
func (m *Session) resourceAuthorizationServers(r *http.Request) []string {
	if m.ProtectedResource != nil && len(m.ProtectedResource.AuthorizationServers) > 0 {
		return m.ProtectedResource.AuthorizationServers
	}

	providerMap := m.Providers()

	names := make([]string, 0, len(providerMap))
	for name, provider := range providerMap {
		if provider.AuthMiddleware != "" {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		left := providerMap[names[i]]
		right := providerMap[names[j]]
		if left.Priority == right.Priority {
			return names[i] < names[j]
		}

		return left.Priority > right.Priority
	})

	seen := map[string]struct{}{}
	servers := []string{}
	for _, name := range names {
		issuer, ok := IssuerRegistry.Get(providerMap[name].AuthMiddleware).(InfIssuerURL)
		if !ok {
			continue
		}

		issuerURL := issuer.IssuerURL(r)
		if issuerURL == "" {
			continue
		}

		if _, dup := seen[issuerURL]; dup {
			continue
		}

		seen[issuerURL] = struct{}{}
		servers = append(servers, issuerURL)
	}

	return servers
}

// serveProtectedResourceMetadata answers the RFC 9728 metadata document.
func (m *Session) serveProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	doc := map[string]any{
		"resource":                 m.resourceID(r),
		"authorization_servers":    m.resourceAuthorizationServers(r),
		"bearer_methods_supported": []string{"header"},
	}

	if m.ProtectedResource != nil && len(m.ProtectedResource.ScopesSupported) > 0 {
		doc["scopes_supported"] = m.ProtectedResource.ScopesSupported
	}

	w.Header().Set("Cache-Control", "public, max-age=300")

	httputil.JSON(w, http.StatusOK, doc)
}

// bearerChallenge builds the WWW-Authenticate value for a 401: a plain
// Bearer challenge, extended with the RFC 9728 resource_metadata pointer
// when protected_resource is configured.
func (m *Session) bearerChallenge(r *http.Request) string {
	if m.ProtectedResource == nil || r == nil {
		return "Bearer"
	}

	return fmt.Sprintf("Bearer resource_metadata=%q", m.resourceMetadataURL(r))
}
