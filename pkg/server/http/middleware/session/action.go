package session

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/worldline-go/klient"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/worldline-go/turna/pkg/server/http/middleware/session/providers"
	"github.com/worldline-go/turna/pkg/server/http/middleware/session/request"
	"github.com/worldline-go/turna/pkg/server/http/tcontext"
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
	// XUser header set from token claims. Default is email and preferred_username.
	// It set first found value.
	XUser            []string `cfg:"x_user"`
	EmailVerifyCheck bool     `cfg:"email_verify_check"`
	// PasswordFlow is use password flow to get token.
	PasswordFlow bool `cfg:"password_flow"`
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
		client, err := klient.New(
			klient.WithDisableBaseURLCheck(true),
			klient.WithDisableRetry(true),
			klient.WithDisableEnvValues(true),
			klient.WithInsecureSkipVerify(m.Action.Token.InsecureSkipVerify),
			klient.WithLogger(slog.Default()),
		)
		if err != nil {
			return fmt.Errorf("cannot create klient: %w", err)
		}

		m.Action.Token.auth.Client = client.HTTP

		providerList := make([]InfProviderCert, 0, len(m.Provider))
		for k, v := range m.Provider {
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

		jwksMulti, err := MultiJWTKeyFunc(providerList)
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

func addXUserHeader(r *http.Request, claim *claims.Custom, xUser []string, emailVerify bool) {
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
				addXUserHeader(r, customClaims, m.Provider[providerName].XUser, m.Provider[providerName].EmailVerifyCheck)

				if v, _ := tcontext.Get(r, CtxTokenHeaderKey).(bool); v {
					r.Header.Del("Authorization")
				}

				next.ServeHTTP(w, r)

				return
			}
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
				provider, ok := m.Provider[providerName]
				if !ok || provider.Oauth2 == nil {
					slog.Error("cannot find provider", "provider", providerName)
					m.RedirectToLogin(w, r, true, true)

					return
				}

				requestConfig := request.AuthRequestConfig{
					TokenURL:     provider.Oauth2.TokenURL,
					ClientID:     provider.Oauth2.ClientID,
					ClientSecret: provider.Oauth2.ClientSecret,
				}

				requestConfig.Scopes = strings.Fields(token.Scope)

				refreshData, err := m.Action.Token.auth.RefreshToken(r.Context(), request.RefreshTokenConfig{
					RefreshToken:      token.RefreshToken,
					AuthRequestConfig: requestConfig,
				})
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

		addXUserHeader(r, customClaims, m.Provider[providerName].XUser, m.Provider[providerName].EmailVerifyCheck)

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
			provider, ok := m.Provider[providerName]
			if !ok || provider.Oauth2 == nil {
				slog.Error("cannot find provider", "provider", providerName)
				return nil, false, fmt.Errorf("cannot find provider %q", providerName)
			}

			requestConfig := request.AuthRequestConfig{
				TokenURL:     provider.Oauth2.TokenURL,
				ClientID:     provider.Oauth2.ClientID,
				ClientSecret: provider.Oauth2.ClientSecret,
			}

			requestConfig.Scopes = strings.Fields(token.Scope)

			refreshData, err := m.Action.Token.auth.RefreshToken(r.Context(), request.RefreshTokenConfig{
				RefreshToken:      token.RefreshToken,
				AuthRequestConfig: requestConfig,
			})
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
