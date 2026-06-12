package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"

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

	auth    request.Auth     `cfg:"-"`
	keyFunc InfKeyFuncParser `cfg:"-"`
}

func (t *Token) GetKeyFunc() InfKeyFuncParser {
	return t.keyFunc
}

type Provider struct {
	Name   string  `cfg:"name"`
	Oauth2 *Oauth2 `cfg:"oauth2"`
	// AuthMiddleware is the name of an in-process auth middleware instance.
	// When set, token validation and refresh go directly to that middleware
	// instead of cert_url/token_url over HTTP. oauth2.client_id should match
	// an OAuth client registered in the auth middleware.
	AuthMiddleware string `cfg:"auth_middleware"`
	// Passkey advertises WebAuthn login on the login page for this provider.
	// Requires auth_middleware (in-process) or oauth2.passkey_url (remote).
	Passkey bool `cfg:"passkey"`
	// XUser header set from token claims. Default is email and preferred_username.
	// It set first found value.
	XUser []string `cfg:"x_user"`
	// ClaimHeader is use to map claim to header.
	//   - Example: claim_header = {"X-User-Id": "preferred_username", "X-User-Email": "email"}
	//   - Default is adding "X-User-Id" header with "preferred_username" claim.
	//   - Set empty value to delete the header.
	ClaimHeader      map[string]string `cfg:"claim_header"`
	EmailVerifyCheck bool              `cfg:"email_verify_check"`
	// PasswordFlow is use password flow to get token.
	PasswordFlow bool `cfg:"password_flow"`
	// APIKey enables static X-API-Key authentication at the session layer.
	// The key is validated directly (in-process via auth_middleware or over
	// oauth2.api_key_url); no token exchange happens and downstream services
	// receive the key principal's claims/X-User.
	APIKey bool `cfg:"api_key"`
	// APIKeyHeader is the header carrying the raw API key. Default X-API-Key.
	APIKeyHeader string `cfg:"api_key_header"`
	// Priority is use to sort provider.
	Priority int `cfg:"priority"`
	// Hide is use to hide provider.
	Hide bool `cfg:"hide"`
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

		providerList := make([]InfProviderCert, 0, len(m.Provider))
		issuerProviders := make(map[string]string)
		for k, v := range m.Provider {
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
			return fmt.Errorf("cannot create keyfunc: %w", err)
		}

		m.Action.Token.keyFunc = jwksMulti
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
	if m.SetProvider != "" {
		p, exists := m.Provider[m.SetProvider]
		if !exists || !p.APIKey {
			return "", Provider{}, "", "", false
		}

		header := apiKeyHeader(p)
		key := r.Header.Get(header)

		return m.SetProvider, p, header, key, key != ""
	}

	names := make([]string, 0, len(m.Provider))
	for name, provider := range m.Provider {
		if provider.APIKey {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		left := m.Provider[names[i]]
		right := m.Provider[names[j]]
		if left.Priority == right.Priority {
			return names[i] < names[j]
		}

		return left.Priority > right.Priority
	})

	for _, name := range names {
		provider := m.Provider[name]
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
		httputil.JSON(w, http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})

		return
	}

	customClaims := &claims.Custom{}
	if err := json.Unmarshal(body, customClaims); err != nil {
		slog.Debug("cannot parse api key claims", "error", err.Error())
		httputil.JSON(w, http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})

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
func (m *Session) refreshTokenData(ctx context.Context, providerName string, token *TokenData) ([]byte, error) {
	provider, ok := m.Provider[providerName]
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

		body, statusCode, err := issuer.IssueToken(ctx, form)
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

	return m.Action.Token.auth.RefreshToken(ctx, request.RefreshTokenConfig{
		RefreshToken:      token.RefreshToken,
		AuthRequestConfig: requestConfig,
	})
}

func (m *Session) Do(next http.Handler, w http.ResponseWriter, r *http.Request) {
	if m.Action.Active == actionToken {
		if authorizationHeader := r.Header.Get("Authorization"); authorizationHeader != "" {
			// get token from header
			if token := strings.TrimPrefix(authorizationHeader, "Bearer "); token != "" {
				// validate token, check if token is valid
				customClaims := &claims.Custom{}
				jwtToken, err := m.Action.Token.keyFunc.ParseWithClaims(token, customClaims)
				if err != nil {
					slog.Debug("token is not valid", "error", err.Error())

					httputil.JSON(w, http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})

					return
				}

				if typ, _ := customClaims.Map["typ"].(string); typ != "" {
					if typ == "Refresh" {
						slog.Debug("token is refresh token")
						httputil.JSON(w, http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})

						return
					}

					if typ == "ID" {
						slog.Debug("token is id token")
						httputil.JSON(w, http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})

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

				tcontext.Set(r, "claims", customClaims)
				tcontext.Set(r, "provider", providerName)
				addXUserHeader(r, customClaims, m.Provider[providerName].XUser, m.Provider[providerName].EmailVerifyCheck, m.Provider[providerName].ClaimHeader)

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
				refreshData, err := m.refreshTokenData(r.Context(), providerName, token)
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
		jwtToken, err := m.Action.Token.keyFunc.ParseWithClaims(token.AccessToken, customClaims)
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

		tcontext.Set(r, "claims", customClaims)
		tcontext.Set(r, "provider", providerName)

		addXUserHeader(r, customClaims, m.Provider[providerName].XUser, m.Provider[providerName].EmailVerifyCheck, m.Provider[providerName].ClaimHeader)

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
		httputil.JSON(w, http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})

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

	provider, ok := m.Provider[providerName]
	if !ok || provider.Oauth2 == nil {
		slog.Error("cannot find provider", "provider", providerName)
		return nil, nil, fmt.Errorf("cannot find provider %q", providerName)
	}

	return token, provider.Oauth2, nil
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
			refreshData, err := m.refreshTokenData(r.Context(), providerName, token)
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
	if _, err := m.Action.Token.keyFunc.ParseWithClaims(token.AccessToken, customClaim); err != nil {
		slog.Debug("token is not valid", "error", err.Error())
		return nil, false, err
	}

	return customClaim, true, nil
}

func (m *Session) RedirectToMain(w http.ResponseWriter, r *http.Request) {
	redirectPath := r.URL.Query().Get("redirect_path")
	if redirectPath == "" {
		redirectPath = "/"
	}

	httputil.Redirect(w, http.StatusTemporaryRedirect, redirectPath)
}
