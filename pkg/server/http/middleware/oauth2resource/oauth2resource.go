// Package oauth2resource protects an upstream service (e.g. an MCP server)
// as an OAuth2 resource server.
//
// It serves the RFC 9728 protected resource metadata document, validates
// bearer access tokens issued by an in-process auth middleware and answers
// unauthenticated requests with the WWW-Authenticate challenge that points
// discovery-driven clients (MCP, RFC 9728) at the metadata.
package oauth2resource

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// wellKnownPath is the RFC 9728 metadata path (path-insertion style).
const wellKnownPath = "/.well-known/oauth-protected-resource"

// DefaultAuthPrefixPath is used to derive the authorization server issuer
// URL when authorization_servers is not configured.
const DefaultAuthPrefixPath = "/auth"

// revocationChecker is implemented by issuers that keep a token denylist
// (the auth middleware); detection is optional and lazy.
type revocationChecker interface {
	TokenRevoked(ctx context.Context, jti string) bool
}

type familyRevocationChecker interface {
	TokenClaimsRevoked(ctx context.Context, jti, sid string) (bool, error)
}

type OAuth2Resource struct {
	// AuthMiddleware is the name of the auth middleware whose tokens are
	// accepted; validation runs in-process through the issuer registry.
	AuthMiddleware string `cfg:"auth_middleware"`

	// Resource is the canonical RFC 8707/9728 resource identifier of the
	// protected API (e.g. https://example.com/mcp). Empty derives
	// {scheme}://{host} from each request.
	Resource string `cfg:"resource"`

	// AuthorizationServers are the issuer URLs listed in the metadata.
	// Empty derives {scheme}://{host}{auth_prefix_path}/oauth2 per request.
	AuthorizationServers []string `cfg:"authorization_servers"`

	// AuthPrefixPath of the auth middleware, used only to derive the issuer
	// when authorization_servers is empty. Default /auth.
	AuthPrefixPath string `cfg:"auth_prefix_path"`

	// ScopesSupported advertised in the metadata document.
	ScopesSupported []string `cfg:"scopes_supported"`

	// RequiredScopes must all be present in the token's scope claim;
	// missing scopes answer 403 insufficient_scope.
	RequiredScopes []string `cfg:"required_scopes"`

	// CheckAudience requires the token aud claim to contain the resource
	// identifier (RFC 8707 binding). Default true.
	CheckAudience *bool `cfg:"check_audience"`

	// CheckRevocation consults the issuer's revocation list on every
	// request. Default true; disable to save a database lookup.
	CheckRevocation *bool `cfg:"check_revocation"`

	// UserHeader receives the authenticated user alias for the upstream.
	// Default X-User.
	UserHeader string `cfg:"user_header"`
	// ScopeHeader receives the granted scopes for the upstream when set.
	ScopeHeader string `cfg:"scope_header"`
}

func (m *OAuth2Resource) getCheckAudience() bool {
	if m.CheckAudience != nil {
		return *m.CheckAudience
	}

	return true
}

func (m *OAuth2Resource) getCheckRevocation() bool {
	if m.CheckRevocation != nil {
		return *m.CheckRevocation
	}

	return true
}

func (m *OAuth2Resource) Middleware() (func(http.Handler) http.Handler, error) {
	if m.AuthMiddleware == "" {
		return nil, fmt.Errorf("oauth2resource requires auth_middleware")
	}

	if m.AuthPrefixPath == "" {
		m.AuthPrefixPath = DefaultAuthPrefixPath
	}
	m.AuthPrefixPath = "/" + strings.Trim(m.AuthPrefixPath, "/")

	if m.UserHeader == "" {
		m.UserHeader = "X-User"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, wellKnownPath) {
				m.metadata(w, r)

				return
			}

			m.protect(w, r, next)
		})
	}, nil
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
func (m *OAuth2Resource) resourceID(r *http.Request) string {
	if m.Resource != "" {
		return m.Resource
	}

	return requestBase(r)
}

// metadataURL builds the RFC 9728 metadata URL for the resource by
// path-insertion on the resource identifier.
func (m *OAuth2Resource) metadataURL(r *http.Request) string {
	resource := m.resourceID(r)

	// split the resource into origin and path; the well-known segment is
	// inserted between them (RFC 9728 §3.1)
	rest := resource
	if idx := strings.Index(resource, "://"); idx >= 0 {
		rest = resource[idx+3:]
	}

	path := ""
	if idx := strings.Index(rest, "/"); idx >= 0 {
		path = rest[idx:]
	}

	origin := strings.TrimSuffix(resource, path)

	return origin + wellKnownPath + strings.TrimSuffix(path, "/")
}

func (m *OAuth2Resource) authorizationServers(r *http.Request) []string {
	if len(m.AuthorizationServers) > 0 {
		return m.AuthorizationServers
	}

	return []string{requestBase(r) + m.AuthPrefixPath + "/oauth2"}
}

// metadata serves the RFC 9728 protected resource metadata document.
func (m *OAuth2Resource) metadata(w http.ResponseWriter, r *http.Request) {
	doc := map[string]any{
		"resource":                 m.resourceID(r),
		"authorization_servers":    m.authorizationServers(r),
		"bearer_methods_supported": []string{"header"},
	}

	if len(m.ScopesSupported) > 0 {
		doc["scopes_supported"] = m.ScopesSupported
	}

	w.Header().Set("Cache-Control", "public, max-age=300")

	httputil.JSON(w, http.StatusOK, doc)
}

// challenge answers 401/403 with the RFC 9728 WWW-Authenticate header.
func (m *OAuth2Resource) challenge(w http.ResponseWriter, r *http.Request, code int, errCode, description string) {
	parts := []string{fmt.Sprintf("resource_metadata=%q", m.metadataURL(r))}
	if errCode != "" {
		parts = append(parts, fmt.Sprintf("error=%q", errCode))
	}
	if description != "" {
		parts = append(parts, fmt.Sprintf("error_description=%q", description))
	}
	if errCode == "insufficient_scope" && len(m.RequiredScopes) > 0 {
		parts = append(parts, fmt.Sprintf("scope=%q", strings.Join(m.RequiredScopes, " ")))
	}

	w.Header().Set("WWW-Authenticate", "Bearer "+strings.Join(parts, ", "))

	httputil.JSON(w, code, map[string]any{
		"error":             errCode,
		"error_description": description,
	})
}

// audienceContains checks a string-or-list aud claim for the resource.
func audienceContains(aud any, resource string) bool {
	switch v := aud.(type) {
	case string:
		return v == resource
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == resource {
				return true
			}
		}
	case []string:
		for _, s := range v {
			if s == resource {
				return true
			}
		}
	}

	return false
}

// protect validates the bearer token and forwards the request upstream.
func (m *OAuth2Resource) protect(w http.ResponseWriter, r *http.Request, next http.Handler) {
	// the identity header must never pass through from the outside
	r.Header.Del(m.UserHeader)
	if m.ScopeHeader != "" {
		r.Header.Del(m.ScopeHeader)
	}

	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if token == "" {
		m.challenge(w, r, http.StatusUnauthorized, "invalid_request", "bearer token required")

		return
	}

	issuer := session.IssuerRegistry.Get(m.AuthMiddleware)
	if issuer == nil {
		httputil.HandleError(w, httputil.NewError(
			fmt.Sprintf("auth middleware %q not found in issuer registry", m.AuthMiddleware),
			nil, http.StatusInternalServerError))

		return
	}

	claims := jwt.MapClaims{}
	opts := []jwt.ParserOption{jwt.WithIssuer(m.authorizationServers(r)[0])}
	if m.getCheckAudience() {
		opts = append(opts, jwt.WithAudience(m.resourceID(r)))
	}
	if _, err := jwt.ParseWithClaims(token, claims, issuer.Keyfunc, opts...); err != nil {
		m.challenge(w, r, http.StatusUnauthorized, "invalid_token", err.Error())

		return
	}

	if typ, _ := claims["typ"].(string); typ != "Bearer" {
		m.challenge(w, r, http.StatusUnauthorized, "invalid_token", "access token type must be Bearer")

		return
	}

	if m.getCheckRevocation() {
		jti, _ := claims["jti"].(string)
		sid, _ := claims["sid"].(string)
		if checker, ok := issuer.(familyRevocationChecker); ok {
			revoked, err := checker.TokenClaimsRevoked(r.Context(), jti, sid)
			if err != nil {
				httputil.HandleError(w, httputil.NewError("cannot check token revocation", err, http.StatusServiceUnavailable))

				return
			}
			if revoked {
				m.challenge(w, r, http.StatusUnauthorized, "invalid_token", "token revoked")

				return
			}
		} else if checker, ok := issuer.(revocationChecker); ok && checker.TokenRevoked(r.Context(), jti) {
			m.challenge(w, r, http.StatusUnauthorized, "invalid_token", "token revoked")

			return
		}
	}

	scope, _ := claims["scope"].(string)
	if len(m.RequiredScopes) > 0 {
		granted := map[string]struct{}{}
		for _, s := range strings.Fields(scope) {
			granted[s] = struct{}{}
		}

		for _, required := range m.RequiredScopes {
			if _, ok := granted[required]; !ok {
				m.challenge(w, r, http.StatusForbidden, "insufficient_scope",
					fmt.Sprintf("scope %q is required", required))

				return
			}
		}
	}

	user, _ := claims["preferred_username"].(string)
	if user == "" {
		user, _ = claims["sub"].(string)
	}

	if user == "" {
		m.challenge(w, r, http.StatusUnauthorized, "invalid_token", "token carries no user identity")

		return
	}

	r.Header.Set(m.UserHeader, user)
	if m.ScopeHeader != "" && scope != "" {
		r.Header.Set(m.ScopeHeader, scope)
	}

	next.ServeHTTP(w, r)
}
