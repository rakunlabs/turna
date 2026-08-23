package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/ok"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/providers"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/request"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

const actionToken = "token"

type Action struct {
	Active string `cfg:"active"`
	Token  *Token `cfg:"token"`
}

type Token struct {
	LoginPath          string `cfg:"login_path"`
	DisableRefresh     bool   `cfg:"disable_refresh"`
	InsecureSkipVerify bool   `cfg:"insecure_skip_verify"`

	// LegacyProxyAuth answers authentication failures with the historic
	// 407 Proxy Authentication Required of the legacy iam stack instead of
	// the standard 401 Unauthorized. Enable it for old deployments whose
	// clients still expect 407.
	LegacyProxyAuth bool `cfg:"legacy_proxy_auth"`

	auth    request.Auth     `cfg:"-"`
	keyFunc InfKeyFuncParser `cfg:"-"`
}

func (t *Token) GetKeyFunc() InfKeyFuncParser {
	return t.keyFunc
}

type Provider struct {
	Name   string  `cfg:"name" json:"name,omitempty"`
	Oauth2 *Oauth2 `cfg:"oauth2" json:"oauth2,omitempty"`
	// AuthMiddleware is the name of an in-process auth middleware instance.
	// When set, token validation and refresh go directly to that middleware
	// instead of cert_url/token_url over HTTP. oauth2.client_id should match
	// an OAuth client registered in the auth middleware.
	AuthMiddleware string `cfg:"auth_middleware" json:"auth_middleware,omitempty"`
	// Passkey advertises WebAuthn login on the login page for this provider.
	// Requires auth_middleware (in-process) or oauth2.passkey_url (remote).
	Passkey bool `cfg:"passkey" json:"passkey,omitempty"`
	// XUser header set from token claims. Default is email and preferred_username.
	// It set first found value.
	XUser []string `cfg:"x_user" json:"x_user,omitempty"`
	// ClaimHeader is use to map claim to header.
	//   - Example: claim_header = {"X-User-Id": "preferred_username", "X-User-Email": "email"}
	//   - Default is adding "X-User-Id" header with "preferred_username" claim.
	//   - Set empty value to delete the header.
	ClaimHeader      map[string]string `cfg:"claim_header" json:"claim_header,omitempty"`
	EmailVerifyCheck bool              `cfg:"email_verify_check" json:"email_verify_check,omitempty"`
	// PasswordFlow is use password flow to get token.
	PasswordFlow bool `cfg:"password_flow" json:"password_flow,omitempty"`
	// APIKey enables static X-API-Key authentication at the session layer.
	// The key is validated directly (in-process via auth_middleware or over
	// oauth2.api_key_url); no token exchange happens and downstream services
	// receive the key principal's claims/X-User.
	APIKey bool `cfg:"api_key" json:"api_key,omitempty"`
	// APIKeyHeader is the header carrying the raw API key. Default X-API-Key.
	APIKeyHeader string `cfg:"api_key_header" json:"api_key_header,omitempty"`
	// Priority is use to sort provider.
	Priority int `cfg:"priority" json:"priority,omitempty"`
	// Hide is use to hide provider.
	Hide bool `cfg:"hide" json:"hide,omitempty"`
}

type ProviderWrapper struct {
	Name    string
	Generic *providers.Generic
}

func (p *ProviderWrapper) GetCertURL() string {
	return p.Generic.CertURL
}

func (p *ProviderWrapper) GetName() string {
	return p.Name
}

// buildKeyFunc builds the token validation keyfunc for a provider map:
// remote JWKS keyfuncs for providers with oauth2.cert_url and an in-process
// issuer keyfunc for providers referencing an auth_middleware.
func buildKeyFunc(providerMap map[string]Provider) (*JwkKeyFuncParse, error) {
	providerList := make([]InfProviderCert, 0, len(providerMap))
	issuerProviders := make(map[string]string)
	for k, v := range providerMap {
		// issuer-backed providers validate tokens in-process; no cert_url needed
		if v.AuthMiddleware != "" {
			issuerProviders[k] = v.AuthMiddleware

			continue
		}

		if v.Oauth2 == nil {
			continue
		}

		providerList = append(providerList, &ProviderWrapper{
			Generic: &providers.Generic{
				CertURL: v.Oauth2.CertURL,
			},
			Name: k,
		})
	}

	opts := []OptionJWK{}
	if len(issuerProviders) > 0 {
		opts = append(opts, WithKeyFunc(&issuerKeyFunc{providers: issuerProviders}))
	}

	jwksMulti, err := MultiJWTKeyFunc(providerList, opts...)
	if err != nil {
		return nil, fmt.Errorf("cannot create keyfunc: %w", err)
	}

	return jwksMulti, nil
}

func (m *Session) SetAction() error {
	if m.Action.Token != nil {
		// set auth client
		client, err := ok.New(
			ok.WithDisableRetry(true),
			ok.WithInsecureSkipVerify(m.Action.Token.InsecureSkipVerify),
			ok.WithLogger(slog.Default()),
		)
		if err != nil {
			return fmt.Errorf("cannot create ok client: %w", err)
		}

		m.Action.Token.auth.Client = client.HTTP

		jwksMulti, err := buildKeyFunc(m.Provider)
		if err != nil {
			// A provider_source is resolved lazily because the referenced auth
			// middleware may register after this session middleware. Allow a
			// source-only configuration to start with an empty static provider
			// map; Do refreshes the source before the keyfunc is used.
			if m.ProviderSource != nil && len(m.Provider) == 0 {
				m.Action.Token.keyFunc = &JwkKeyFuncParse{
					KeyFunc: func(_ *jwt.Token) (any, error) {
						return nil, ErrKIDNotFound
					},
				}
			} else {
				return err
			}
		} else {
			m.Action.Token.keyFunc = jwksMulti
		}
	}

	// set active action
	if m.Action.Active != "" {
		return nil
	}

	if m.Action.Token != nil {
		m.Action.Active = actionToken

		return nil
	}

	return nil
}

func (m *Session) GetCookieName(r *http.Request) string {
	if v, ok := tcontext.Get(r, CtxCookieNameKey).(string); ok && v != "" {
		return v
	}

	cookieName := m.CookieName

	if len(m.CookieNameHosts) > 0 {
		host := r.Host

		for _, v := range m.CookieNameHosts {
			if v.rgx != nil {
				if v.rgx.MatchString(host) {
					cookieName = v.CookieName

					break
				}
			} else {
				if v.Host == host {
					cookieName = v.CookieName

					break
				}
			}
		}
	}

	return cookieName
}

func addXUserHeader(r *http.Request, claim *claims.Custom, xUser []string, emailVerify bool, customClaimHeader map[string]string) {
	r.Header.Del("X-User")

	if len(xUser) == 0 {
		xUser = []string{"email", "preferred_username", "name"}
	}

	for _, v := range xUser {
		if claimValue, ok := claim.Map[v].(string); ok {
			if v == "email" && emailVerify && claim.Map["email_verified"] != true {
				continue
			}

			r.Header.Set("X-User", claimValue)

			break
		}
	}

	// Add X-User-Id header with preferred_username claim if exist and not set in custom claim header.
	r.Header.Del("X-User-Id")

	if claimValue, ok := claim.Map["preferred_username"].(string); ok {
		r.Header.Set("X-User-Id", claimValue)
	}

	// add custom claim headers
	if len(customClaimHeader) > 0 {
		for k, v := range customClaimHeader {
			r.Header.Del(k)

			if headerValue, ok := claim.Map[v].(string); ok {
				r.Header.Set(k, headerValue)
			}
		}
	}
}

func apiKeyHeader(provider Provider) string {
	if provider.APIKeyHeader != "" {
		return provider.APIKeyHeader
	}

	return "X-API-Key"
}

func (m *Session) apiKeyRequest(r *http.Request) (providerName string, provider Provider, headerName string, key string, ok bool) {
	providerMap := m.Providers()

	if m.SetProvider != "" {
		p, exists := providerMap[m.SetProvider]
		if !exists || !p.APIKey {
			return "", Provider{}, "", "", false
		}

		header := apiKeyHeader(p)
		key := r.Header.Get(header)

		return m.SetProvider, p, header, key, key != ""
	}

	names := make([]string, 0, len(providerMap))
	for name, provider := range providerMap {
		if provider.APIKey {
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

	for _, name := range names {
		provider := providerMap[name]
		header := apiKeyHeader(provider)
		key := r.Header.Get(header)
		if key != "" {
			return name, provider, header, key, true
		}
	}

	return "", Provider{}, "", "", false
}

// apiKeyClaimsData validates a raw static api key and returns claim-shaped
// identity JSON; no token exchange happens. In-process issuers check their
// database directly, remote providers are called over oauth2.api_key_url.
func (m *Session) apiKeyClaimsData(ctx context.Context, providerName string, provider Provider, key string) ([]byte, error) {
	if provider.AuthMiddleware != "" {
		issuer := IssuerRegistry.Get(provider.AuthMiddleware)
		if issuer == nil {
			return nil, fmt.Errorf("issuer %q not found", provider.AuthMiddleware)
		}

		validator, ok := issuer.(InfAPIKey)
		if !ok {
			return nil, fmt.Errorf("issuer %q does not support api keys", provider.AuthMiddleware)
		}

		return validator.APIKeyData(ctx, key)
	}

	if provider.Oauth2 == nil || provider.Oauth2.APIKeyURL == "" {
		return nil, fmt.Errorf("provider %q has no api_key_url", providerName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.Oauth2.APIKeyURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", key)
	req.Header.Set("Accept", "application/json")

	return m.Action.Token.auth.RawRequest(req)
}

// serveAPIKey authenticates the request with a static api key: the key is
// validated, the raw key header is removed, and downstream sees the usual
// claims context and X-User headers.
func (m *Session) serveAPIKey(next http.Handler, w http.ResponseWriter, r *http.Request, providerName string, provider Provider, headerName, key string) {
	body, err := m.apiKeyClaimsData(r.Context(), providerName, provider, key)
	if err != nil {
		slog.Debug("api key validation failed", "error", err.Error())
		m.unauthorized(w)

		return
	}

	customClaims := &claims.Custom{}
	if err := json.Unmarshal(body, customClaims); err != nil {
		slog.Debug("cannot parse api key claims", "error", err.Error())
		m.unauthorized(w)

		return
	}

	r.Header.Del(headerName)

	tcontext.Set(r, "claims", customClaims)
	tcontext.Set(r, "provider", providerName)
	addXUserHeader(r, customClaims, provider.XUser, provider.EmailVerifyCheck, provider.ClaimHeader)

	next.ServeHTTP(w, r)
}

// refreshTokenData refreshes the access token of the provider, either
// in-process through a registered issuer (auth_middleware) or over HTTP
// against the provider's token_url.
func (m *Session) refreshTokenData(r *http.Request, providerName string, token *TokenData) ([]byte, error) {
	// Auth rotates refresh tokens and rejects reuse. Collapse simultaneous
	// requests carrying the same session cookie so they all receive the one
	// successful refresh response instead of racing each other.
	key := fmt.Sprintf("%s:%x", providerName, sha256.Sum256([]byte(token.RefreshToken)))
	value, err, _ := m.refreshGroup.Do(key, func() (any, error) {
		return m.refreshTokenDataOnce(r, providerName, token)
	})
	if err != nil {
		return nil, err
	}

	return value.([]byte), nil
}

func (m *Session) refreshTokenDataOnce(r *http.Request, providerName string, token *TokenData) ([]byte, error) {
	provider, ok := m.GetProvider(providerName)
	if !ok {
		return nil, fmt.Errorf("cannot find provider %q", providerName)
	}

	if provider.AuthMiddleware != "" {
		issuer := IssuerRegistry.Get(provider.AuthMiddleware)
		if issuer == nil {
			return nil, fmt.Errorf("issuer %q not found", provider.AuthMiddleware)
		}

		form := url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {token.RefreshToken},
		}
		if provider.Oauth2 != nil {
			form.Set("client_id", provider.Oauth2.ClientID)
			if provider.Oauth2.ClientSecret != "" {
				form.Set("client_secret", provider.Oauth2.ClientSecret)
			}
		}
		if token.Scope != "" {
			form.Set("scope", token.Scope)
		}

		body, statusCode, err := issuer.IssueToken(r, form)
		if err != nil {
			return nil, err
		}
		if statusCode < 200 || statusCode > 299 {
			return nil, fmt.Errorf("refresh token failed: %s", string(body))
		}

		return body, nil
	}

	if provider.Oauth2 == nil {
		return nil, fmt.Errorf("cannot find provider %q", providerName)
	}

	requestConfig := request.AuthRequestConfig{
		TokenURL:     provider.Oauth2.TokenURL,
		ClientID:     provider.Oauth2.ClientID,
		ClientSecret: provider.Oauth2.ClientSecret,
	}

	requestConfig.Scopes = strings.Fields(token.Scope)

	return m.Action.Token.auth.RefreshToken(r.Context(), request.RefreshTokenConfig{
		RefreshToken:      token.RefreshToken,
		AuthRequestConfig: requestConfig,
	})
}

// unauthorized answers an authentication failure: standard 401 with a
// Bearer challenge by default, or the historic 407 Proxy Authentication
// Required when legacy_proxy_auth is enabled.
func (m *Session) unauthorized(w http.ResponseWriter) {
	status := http.StatusUnauthorized

	if m.Action.Token != nil && m.Action.Token.LegacyProxyAuth {
		status = http.StatusProxyAuthRequired
	} else {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}

	httputil.JSON(w, status, MetaData{Error: http.StatusText(status)})
}

// skipPath reports whether the request path matches an explicit skip_paths
// pattern or a public-plane pattern published by an in-process issuer.
func (m *Session) skipPath(path string) bool {
	for _, pattern := range m.SkipPaths {
		if ok, _ := doublestar.Match(pattern, path); ok {
			return true
		}
	}

	for _, pattern := range m.issuerSkipPatterns() {
		if ok, _ := doublestar.Match(pattern, path); ok {
			return true
		}
	}

	return false
}

// issuerSkipPatterns collects the public path patterns of every issuer this
// middleware's providers reference with auth_middleware, so their machine
// endpoints (token, callbacks, discovery, consent) never get captured by an
// interactive login redirect.
//
// With a static provider map the set is resolved lazily on the first request
// and then cached: issuers register themselves while middlewares are built
// and requests only arrive after the server starts (same assumption as
// issuerKeyFunc). With a provider_source the set lives in the dynamic state
// and is recomputed when the provider set changes.
func (m *Session) issuerSkipPatterns() []string {
	if m.DisableIssuerSkipPaths {
		return nil
	}

	if m.ProviderSource != nil {
		if st := m.dynamic.Load(); st != nil {
			return st.skipPaths
		}
	}

	m.issuerSkipOnce.Do(func() {
		m.issuerSkipPaths = issuerSkipPatternsFor(m.Provider)
	})

	return m.issuerSkipPaths
}

// issuerSkipPatternsFor collects the deduplicated public path patterns of
// every issuer the given providers reference with auth_middleware.
func issuerSkipPatternsFor(providers map[string]Provider) []string {
	seen := map[string]struct{}{}
	patterns := []string{}

	for _, provider := range providers {
		if provider.AuthMiddleware == "" {
			continue
		}

		issuer, ok := IssuerRegistry.Get(provider.AuthMiddleware).(InfPublicPaths)
		if !ok {
			continue
		}

		for _, pattern := range issuer.PublicPathPatterns() {
			if _, dup := seen[pattern]; dup || pattern == "" {
				continue
			}

			seen[pattern] = struct{}{}
			patterns = append(patterns, pattern)
		}
	}

	return patterns
}

// stripIdentityHeaders removes every header the session middleware would set
// from claims, so anonymous pass-through requests cannot spoof identity.
func (m *Session) stripIdentityHeaders(r *http.Request) {
	r.Header.Del("X-User")
	r.Header.Del("X-User-Id")

	for _, provider := range m.Providers() {
		for k := range provider.ClaimHeader {
			r.Header.Del(k)
		}
	}
}

var errNoSession = errors.New("session cookie not found")

// cookieClaims validates the session cookie (refreshing the token when
// needed) and returns the claims with the provider name and token. It never
// writes an error response; callers decide between redirect and anonymous
// pass-through.
func (m *Session) cookieClaims(w http.ResponseWriter, r *http.Request) (*claims.Custom, string, *TokenData, error) {
	token64 := ""
	providerName := ""
	if v, err := m.store.Get(r, m.GetCookieName(r)); !v.IsNew && err == nil {
		token64, _ = v.Values[TokenKey].(string)
		if m.SetProvider != "" {
			providerName = m.SetProvider
		} else {
			providerName, _ = v.Values[ProviderKey].(string)
		}
	} else {
		if err != nil {
			return nil, "", nil, err
		}

		return nil, "", nil, errNoSession
	}

	token, err := ParseToken64(token64)
	if err != nil {
		return nil, "", nil, err
	}

	if !m.Action.Token.DisableRefresh {
		v, err := IsRefreshNeed(token.AccessToken)
		if err != nil {
			return nil, "", nil, err
		}

		if v {
			refreshData, err := m.refreshTokenData(r, providerName, token)
			if err != nil {
				return nil, "", nil, err
			}

			if err := m.SetToken(w, r, refreshData, providerName); err != nil {
				return nil, "", nil, err
			}

			token, err = ParseToken(refreshData)
			if err != nil {
				return nil, "", nil, err
			}
		}
	}

	customClaims := &claims.Custom{}
	jwtToken, err := m.KeyFuncParser().ParseWithClaims(token.AccessToken, customClaims)
	if err != nil {
		return nil, "", nil, err
	}

	if m.SetProvider != "" {
		providerName = m.SetProvider
	} else {
		providerName, _ = jwtToken.Header["provider_name"].(string)
	}

	return customClaims, providerName, token, nil
}

// doOptional handles skip_paths requests: authentication is attempted with
// the usual sources (bearer, api key, cookie) and applied when it succeeds,
// but failures and anonymous requests pass through with identity headers
// stripped instead of a redirect or 407.
func (m *Session) doOptional(next http.Handler, w http.ResponseWriter, r *http.Request) {
	anonymous := func() {
		m.stripIdentityHeaders(r)
		next.ServeHTTP(w, r)
	}

	if authorizationHeader := r.Header.Get("Authorization"); authorizationHeader != "" {
		token := strings.TrimPrefix(authorizationHeader, "Bearer ")
		if token == "" {
			anonymous()

			return
		}

		customClaims := &claims.Custom{}
		jwtToken, err := m.KeyFuncParser().ParseWithClaims(token, customClaims)
		if err != nil {
			anonymous()

			return
		}

		if typ, _ := customClaims.Map["typ"].(string); typ == "Refresh" || typ == "ID" {
			anonymous()

			return
		}

		providerName := m.SetProvider
		if providerName == "" {
			providerName, _ = jwtToken.Header["provider_name"].(string)
		}

		provider, _ := m.GetProvider(providerName)

		tcontext.Set(r, "claims", customClaims)
		tcontext.Set(r, "provider", providerName)
		addXUserHeader(r, customClaims, provider.XUser, provider.EmailVerifyCheck, provider.ClaimHeader)

		if v, _ := tcontext.Get(r, CtxTokenHeaderKey).(bool); v {
			r.Header.Del("Authorization")
		}

		next.ServeHTTP(w, r)

		return
	}

	if providerName, provider, headerName, key, ok := m.apiKeyRequest(r); ok {
		body, err := m.apiKeyClaimsData(r.Context(), providerName, provider, key)
		if err != nil {
			anonymous()

			return
		}

		customClaims := &claims.Custom{}
		if err := json.Unmarshal(body, customClaims); err != nil {
			anonymous()

			return
		}

		r.Header.Del(headerName)

		tcontext.Set(r, "claims", customClaims)
		tcontext.Set(r, "provider", providerName)
		addXUserHeader(r, customClaims, provider.XUser, provider.EmailVerifyCheck, provider.ClaimHeader)

		next.ServeHTTP(w, r)

		return
	}

	customClaims, providerName, token, err := m.cookieClaims(w, r)
	if err != nil {
		if !errors.Is(err, errNoSession) {
			slog.Debug("session skip path: cookie auth failed", "error", err.Error())
		}

		anonymous()

		return
	}

	provider, _ := m.GetProvider(providerName)

	tcontext.Set(r, "claims", customClaims)
	tcontext.Set(r, "provider", providerName)
	addXUserHeader(r, customClaims, provider.XUser, provider.EmailVerifyCheck, provider.ClaimHeader)

	if v, _ := tcontext.Get(r, CtxTokenHeaderKey).(bool); v {
		r.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}

	if v, _ := tcontext.Get(r, CtxTokenHeaderDelKey).(bool); v {
		r.Header.Del("Authorization")
	}

	next.ServeHTTP(w, r)
}

func (m *Session) Do(next http.Handler, w http.ResponseWriter, r *http.Request) {
	// bring the dynamic provider list up to date before any decision below
	// (skip paths, keyfunc, provider lookups) relies on it.
	m.providerRefresh()

	if m.Action.Active == actionToken && m.skipPath(r.URL.Path) {
		m.doOptional(next, w, r)

		return
	}

	if m.Action.Active == actionToken {
		if authorizationHeader := r.Header.Get("Authorization"); authorizationHeader != "" {
			// get token from header
			if token := strings.TrimPrefix(authorizationHeader, "Bearer "); token != "" {
				// validate token, check if token is valid
				customClaims := &claims.Custom{}
				jwtToken, err := m.KeyFuncParser().ParseWithClaims(token, customClaims)
				if err != nil {
					slog.Debug("token is not valid", "error", err.Error())

					m.unauthorized(w)

					return
				}

				if typ, _ := customClaims.Map["typ"].(string); typ != "" {
					if typ == "Refresh" {
						slog.Debug("token is refresh token")
						m.unauthorized(w)

						return
					}

					if typ == "ID" {
						slog.Debug("token is id token")
						m.unauthorized(w)

						return
					}
				}

				// next middlewares can check roles
				var providerName string
				if m.SetProvider != "" {
					providerName = m.SetProvider
				} else {
					providerName, _ = jwtToken.Header["provider_name"].(string)
				}

				provider, _ := m.GetProvider(providerName)

				tcontext.Set(r, "claims", customClaims)
				tcontext.Set(r, "provider", providerName)
				addXUserHeader(r, customClaims, provider.XUser, provider.EmailVerifyCheck, provider.ClaimHeader)

				if v, _ := tcontext.Get(r, CtxTokenHeaderKey).(bool); v {
					r.Header.Del("Authorization")
				}

				next.ServeHTTP(w, r)

				return
			}
		}

		if providerName, provider, headerName, key, ok := m.apiKeyRequest(r); ok {
			m.serveAPIKey(next, w, r, providerName, provider, headerName, key)

			return
		}

		// get token from store
		// if not exist, redirect to login page with redirect url
		// set token to the header and continue

		// check if token exist in store
		token64 := ""
		providerName := ""
		if v, err := m.store.Get(r, m.GetCookieName(r)); !v.IsNew && err == nil {
			// add the access token to the request
			token64, _ = v.Values[TokenKey].(string)
			if m.SetProvider != "" {
				providerName = m.SetProvider
			} else {
				providerName, _ = v.Values[ProviderKey].(string)
			}
		} else {
			if err != nil {
				slog.Error("cannot get session", "error", err.Error())
			}

			// cookie not found, redirect to login page
			m.RedirectToLogin(w, r, true, false)

			return
		}

		// check if token is valid
		token, err := ParseToken64(token64)
		if err != nil {
			slog.Error("cannot parse token", "error", err.Error())
			m.RedirectToLogin(w, r, true, true)

			return
		}

		// check if token is expired
		if !m.Action.Token.DisableRefresh {
			v, err := IsRefreshNeed(token.AccessToken)
			if err != nil {
				slog.Error("cannot check if token is expired", "error", err.Error())
				m.RedirectToLogin(w, r, true, true)

				return
			}

			if v {
				refreshData, err := m.refreshTokenData(r, providerName, token)
				if err != nil {
					slog.Error("cannot refresh token", "error", err.Error())
					m.RedirectToLogin(w, r, true, true)

					return
				}

				// set new token to the store
				if err := m.SetToken(w, r, refreshData, providerName); err != nil {
					slog.Error("cannot set session", "error", err.Error())
					m.RedirectToLogin(w, r, true, true)

					return
				}

				// add the access token to the request
				token, err = ParseToken(refreshData)
				if err != nil {
					slog.Error("cannot parse token", "error", err.Error())
					m.RedirectToLogin(w, r, true, true)

					return
				}
			}
		}

		// check if token is valid
		customClaims := &claims.Custom{}
		jwtToken, err := m.KeyFuncParser().ParseWithClaims(token.AccessToken, customClaims)
		if err != nil {
			slog.Debug("token is not valid", "error", err.Error())
			m.RedirectToLogin(w, r, true, true)

			return
		}

		// next middlewares can check roles
		if m.SetProvider != "" {
			providerName = m.SetProvider
		} else {
			providerName, _ = jwtToken.Header["provider_name"].(string)
		}

		provider, _ := m.GetProvider(providerName)

		tcontext.Set(r, "claims", customClaims)
		tcontext.Set(r, "provider", providerName)

		addXUserHeader(r, customClaims, provider.XUser, provider.EmailVerifyCheck, provider.ClaimHeader)

		// add the access token to the request
		if v, _ := tcontext.Get(r, CtxTokenHeaderKey).(bool); v {
			r.Header.Set("Authorization", "Bearer "+token.AccessToken)
		}

		if v, _ := tcontext.Get(r, CtxTokenHeaderDelKey).(bool); v {
			r.Header.Del("Authorization")
		}

		next.ServeHTTP(w, r)

		return
	}

	httputil.JSON(w, http.StatusNotFound, MetaData{Error: fmt.Sprintf("action %q not found", m.Action.Active)})
}

func (m *Session) RedirectToLogin(w http.ResponseWriter, r *http.Request, addRedirectPath bool, removeSession bool) {
	// check redirection is disabled
	if v, _ := tcontext.Get(r, CtxDisableRedirectKey).(bool); v {
		m.unauthorized(w)

		return
	}

	if removeSession {
		if err := m.DelToken(w, r); err != nil {
			slog.Error("cannot remove session", "error", err.Error())
		}
	}

	// add redirect_path query param
	if !addRedirectPath {
		httputil.Redirect(w, http.StatusTemporaryRedirect, m.Action.Token.LoginPath)

		return
	}

	httputil.Redirect(w, http.StatusTemporaryRedirect, loginPathWithRedirect(r, m.Action.Token.LoginPath))
}

func loginPathWithRedirect(r *http.Request, loginPath string) string {
	redirectPath := r.URL.Path
	if r.URL.RawQuery != "" {
		redirectPath = fmt.Sprintf("%s?%s", redirectPath, r.URL.RawQuery)
	}

	if (redirectPath == "" || redirectPath == "/") && r.URL.RawQuery == "" {
		return loginPath
	}

	return fmt.Sprintf("%s?redirect_path=%s", loginPath, url.QueryEscape(redirectPath))
}

func (m *Session) GetToken(r *http.Request) (*TokenData, *Oauth2, error) {
	// check if token exist in store
	v64 := ""
	providerName := ""
	if v, err := m.store.Get(r, m.GetCookieName(r)); !v.IsNew && err == nil {
		// add the access token to the request
		v64, _ = v.Values[TokenKey].(string)
		providerName, _ = v.Values[ProviderKey].(string)
	} else {
		if err != nil {
			return nil, nil, err
		}

		// cookie not found, redirect to login page
		return nil, nil, fmt.Errorf("cookie not found")
	}

	if m.SetProvider != "" {
		providerName = m.SetProvider
	}

	// check if token is valid
	token, err := ParseToken64(v64)
	if err != nil {
		slog.Error("cannot parse token", "error", err.Error())
		return nil, nil, err
	}

	provider, ok := m.GetProvider(providerName)
	if !ok || provider.Oauth2 == nil {
		slog.Error("cannot find provider", "provider", providerName)
		return nil, nil, fmt.Errorf("cannot find provider %q", providerName)
	}

	return token, provider.Oauth2, nil
}

// GetTokenData returns the stored token with its provider name. Unlike
// GetToken it does not require the provider to have an oauth2 block, so it
// also works for issuer-backed (auth_middleware) providers.
func (m *Session) GetTokenData(r *http.Request) (*TokenData, string, error) {
	v, err := m.store.Get(r, m.GetCookieName(r))
	if err != nil {
		return nil, "", err
	}
	if v.IsNew {
		return nil, "", errNoSession
	}

	v64, _ := v.Values[TokenKey].(string)
	providerName, _ := v.Values[ProviderKey].(string)
	if m.SetProvider != "" {
		providerName = m.SetProvider
	}

	token, err := ParseToken64(v64)
	if err != nil {
		return nil, "", err
	}

	return token, providerName, nil
}

// IsLogged check token is exist and valid.
func (m *Session) IsLogged(w http.ResponseWriter, r *http.Request) (*claims.Custom, bool, error) {
	// check if token exist in store
	v64 := ""
	providerName := ""
	if v, err := m.store.Get(r, m.GetCookieName(r)); !v.IsNew && err == nil {
		// add the access token to the request
		v64, _ = v.Values[TokenKey].(string)
		providerName, _ = v.Values[ProviderKey].(string)
	} else {
		if err != nil {
			return nil, false, err
		}

		// cookie not found, redirect to login page
		return nil, false, nil
	}

	if m.SetProvider != "" {
		providerName = m.SetProvider
	}

	// check if token is valid
	token, err := ParseToken64(v64)
	if err != nil {
		slog.Error("cannot parse token", "error", err.Error())
		return nil, false, err
	}

	// check if token is expired
	if !m.Action.Token.DisableRefresh {
		v, err := IsRefreshNeed(token.AccessToken)
		if err != nil {
			slog.Error("cannot check if token is expired", "error", err.Error())
			return nil, false, err
		}

		if v {
			refreshData, err := m.refreshTokenData(r, providerName, token)
			if err != nil {
				slog.Error("cannot refresh token", "error", err.Error())
				return nil, false, err
			}

			// set new token to the store
			if err := m.SetToken(w, r, refreshData, providerName); err != nil {
				slog.Error("cannot set session", "error", err.Error())
				return nil, false, err
			}

			// add the access token to the request
			token, err = ParseToken(refreshData)
			if err != nil {
				slog.Error("cannot parse token", "error", err.Error())
				return nil, false, err
			}
		}
	}

	// check if token is valid
	customClaim := &claims.Custom{}
	if _, err := m.KeyFuncParser().ParseWithClaims(token.AccessToken, customClaim); err != nil {
		slog.Debug("token is not valid", "error", err.Error())
		return nil, false, err
	}

	return customClaim, true, nil
}

func (m *Session) RedirectToMain(w http.ResponseWriter, r *http.Request) {
	httputil.Redirect(w, http.StatusTemporaryRedirect, safeRedirectPath(r.URL.Query().Get("redirect_path")))
}

func safeRedirectPath(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") ||
		strings.HasPrefix(parsed.Path, "//") || strings.Contains(parsed.Path, `\`) {
		return "/"
	}

	return parsed.String()
}
