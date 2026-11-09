package auth

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// APICerts returns the JWKS document.
func (m *Auth) APICerts(w http.ResponseWriter, r *http.Request) {
	signer, err := m.jwtRuntime(r.Context())
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	keys := signer.VerifyKeys(time.Now())
	jwkList := make([]JWK, 0, len(keys))
	for _, k := range keys {
		jwkList = append(jwkList, JWK{
			KID: k.KID,
			KTY: "RSA",
			ALG: signer.JWT.Alg(),
			Use: "sig",
			N:   base64.RawURLEncoding.EncodeToString(k.Public.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.Public.E)).Bytes()),
		})
	}

	w.Header().Set("Cache-Control", "public, max-age=300")

	httputil.JSON(w, http.StatusOK, JWKSResponse{Keys: jwkList})
}

// serverMetadata builds the shared OIDC discovery / RFC 8414 authorization
// server metadata document.
func (m *Auth) serverMetadata(r *http.Request, custom string) map[string]any {
	issuer := m.issuerURL(r)
	sn := m.cache.Snapshot()

	alg := "RS256"
	if signer, err := m.jwtRuntime(r.Context()); err == nil {
		alg = signer.JWT.Alg()
	}

	// issuer stays the shared /oauth2 value (matches the id_token iss); a
	// {custom} segment only points userinfo at the per-name custom_info route
	// so discovery-driven clients pick up the tailored claims automatically.
	userinfoEndpoint := issuer + "/userinfo"
	if custom != "" {
		userinfoEndpoint = issuer + "/userinfo/" + url.PathEscape(custom)
	}

	metadata := map[string]any{
		"issuer": issuer,
		"authorization_response_iss_parameter_supported": true,
		"authorization_endpoint":                         issuer + "/authorize",
		"token_endpoint":                                 issuer + "/token",
		"userinfo_endpoint":                              userinfoEndpoint,
		"jwks_uri":                                       issuer + "/certs",
		"device_authorization_endpoint":                  issuer + "/device_authorization",
		"revocation_endpoint":                            issuer + "/revoke",
		"end_session_endpoint":                           issuer + "/logout",
		"pushed_authorization_request_endpoint":          issuer + "/par",
		"dpop_signing_alg_values_supported":              dpopAlgorithms,
		"require_pushed_authorization_requests":          sn.Authorize.RequirePAR,
		"introspection_endpoint":                         issuer + "/introspect",
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported": []string{
			"authorization_code", "client_credentials", "password", "refresh_token",
			grantTypeDeviceCode, grantTypeTokenExchange, grantTypeEmailCode,
		},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{alg},
		"code_challenge_methods_supported":      pkceMethods(sn.Authorize.AllowPlainPKCE),
		"client_id_metadata_document_supported": !sn.Registration.ClientMetadataDisabled,
		"scopes_supported":                      append([]string{"openid"}, standardScopes...),
		"claims_supported": []string{
			"iss", "sub", "aud", "exp", "iat", "auth_time", "nonce", "azp", "sid",
			"name", "given_name", "family_name", "middle_name", "nickname", "preferred_username",
			"profile", "picture", "website", "gender", "birthdate", "zoneinfo", "locale", "updated_at",
			"email", "email_verified", "phone_number", "phone_number_verified", "address",
		},
		"claims_parameter_supported":      false,
		"request_parameter_supported":     false,
		"request_uri_parameter_supported": false,
		"prompt_values_supported":         []string{"none", "login", "consent"},
		"token_endpoint_auth_methods_supported": []string{
			"client_secret_basic", "client_secret_post", "none",
		},
		"revocation_endpoint_auth_methods_supported": []string{
			"client_secret_basic", "client_secret_post",
		},
		"introspection_endpoint_auth_methods_supported": []string{
			"client_secret_basic", "client_secret_post",
		},
	}

	if sn.Registration.Enabled {
		metadata["registration_endpoint"] = issuer + "/register"
	}

	return metadata
}

// pkceMethods lists the advertised code_challenge_method values.
func pkceMethods(allowPlain bool) []string {
	if allowPlain {
		return []string{"S256", "plain"}
	}

	return []string{"S256"}
}

// APIWellKnown returns the OpenID configuration for this issuer.
func (m *Auth) APIWellKnown(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, m.serverMetadata(r, r.PathValue("custom")))
}

// APIAuthServerMetadata returns the RFC 8414 authorization server metadata.
// It serves both the path-suffix variant under the issuer and the RFC-style
// root path-insertion variant (/.well-known/oauth-authorization-server/...).
func (m *Auth) APIAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, m.serverMetadata(r, ""))
}

// IssuerURL exposes the canonical external issuer URL of this auth
// instance (session.InfIssuerURL); session middlewares use it to derive
// authorization_servers of their RFC 9728 protected resource metadata.
func (m *Auth) IssuerURL(r *http.Request) string {
	return m.issuerURL(r)
}

// AcceptsIssuer reports whether iss names this Auth instance on any public
// host: {scheme}://{host}{prefix}/oauth2. The issuer URL follows the host a
// token was requested through (or oauth2.base_url after host replacement), so
// a token signed with this instance's key stays valid when a client logs in
// through one host and refreshes or calls userinfo through another. The
// signature proves the token is ours; the prefix separates Auth instances
// sharing the same key. It implements session.InfIssuerAcceptor.
func (m *Auth) AcceptsIssuer(iss string) bool {
	u, err := url.Parse(iss)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}

	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.Path == m.PrefixPath+"/oauth2"
}

// parseOwnToken validates a token signed by this instance for its own
// endpoints (refresh, userinfo, introspection, token exchange).
func (m *Auth) parseOwnToken(signer *jwtSigner, raw string, claims *jwt.MapClaims) error {
	if _, err := signer.Parse(raw, claims, jwt.WithAudience("turna-auth")); err != nil {
		return err
	}

	if iss, _ := (*claims)["iss"].(string); !m.AcceptsIssuer(iss) {
		return fmt.Errorf("token validate: %w", jwt.ErrTokenInvalidIssuer)
	}

	return nil
}

// effectiveOAuthBaseURL applies this instance's endpoint host mapping to the
// stored canonical OAuth origin. Every externally published OAuth URL must use
// this same value; applying the mapping only to an upstream callback would make
// discovery, token issuers and redirect_uri disagree about the public host.
func (m *Auth) effectiveOAuthBaseURL() string {
	if m.cache == nil {
		return ""
	}

	cfg := m.cache.Snapshot().OAuth2
	// An unusable stored origin falls back to the request instead of
	// publishing a relative or scheme-less redirect_uri.
	baseURL, err := normalizeOAuthBaseURL(cfg.BaseURL, cfg.Schema)
	if err != nil || baseURL == "" {
		return ""
	}

	return session.ReplaceEndpointHost(baseURL, m.SessionProvidersConfig.HostReplacements)
}

// normalizeOAuthBaseURL reduces oauth2.base_url to {scheme}://{host}. A value
// without a scheme ("auth.example.com") gets oauth2.schema (default https);
// otherwise the host would parse as a path, host replacement would never
// match and upstream callbacks would become relative URLs.
func normalizeOAuthBaseURL(raw, schema string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	if !strings.Contains(raw, "://") {
		if schema == "" {
			schema = "https"
		}
		raw = schema + "://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("base_url scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("base_url host is required")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("base_url must be an origin like https://auth.example.com without path, query or fragment")
	}

	return parsed.Scheme + "://" + parsed.Host, nil
}

// validateOAuth2Settings rejects origins that would silently break the
// published OAuth surface.
func validateOAuth2Settings(setting OAuth2Settings) error {
	switch setting.Schema {
	case "", "http", "https":
	default:
		return fmt.Errorf("schema must be http or https")
	}

	_, err := normalizeOAuthBaseURL(setting.BaseURL, setting.Schema)

	return err
}

// requestOriginWarn logs once that public URLs follow the request.
var requestOriginWarn sync.Once

func (m *Auth) issuerURL(r *http.Request) string {
	// oauth2.base_url is the canonical external origin of this auth
	// instance after its instance-local host replacement. Use it for discovery,
	// token issuers and upstream code callbacks so every protocol surface agrees.
	if baseURL := m.effectiveOAuthBaseURL(); baseURL != "" {
		return baseURL + m.PrefixPath + "/oauth2"
	}

	requestOriginWarn.Do(func() {
		slog.Warn("auth: oauth2 base_url is not set, the issuer is built from the request host and forwarded headers; set base_url or use real_ip with trusted_proxies")
	})

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

	return fmt.Sprintf("%s://%s%s/oauth2", scheme, host, m.PrefixPath)
}
