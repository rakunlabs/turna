package session

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth"
	"github.com/worldline-go/auth/claims"
	"github.com/worldline-go/auth/providers"
	"github.com/worldline-go/auth/request"
	"github.com/worldline-go/klient"
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

func (m *Session) GetCookieName(c echo.Context) string {
	if v, ok := c.Get(CtxCookieNameKey).(string); ok && v != "" {
		return v
	}

	cookieName := m.CookieName

	if len(m.CookieNameHosts) > 0 {
		host := c.Request().Host

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
		xUser = []string{"email", "preferred_username"}
	}

	for _, v := range xUser {
		if claimValue, ok := claim.Map[v].(string); ok {
			if v == "email" && emailVerify && claim.Map["email_verified"] != "true" {
				continue
			}

			r.Header.Set("X-User", claimValue)

			break
		}
	}
}

func (m *Session) Do(next echo.HandlerFunc, c echo.Context) error {
	if m.Action.Active == actionToken {
		if authorizationHeader := c.Request().Header.Get("Authorization"); authorizationHeader != "" {
			// get token from header
			if token := strings.TrimPrefix(authorizationHeader, "Bearer "); token != "" {
				// validate token, check if token is valid
				customClaims := &claims.Custom{}
				jwtToken, err := m.Action.Token.keyFunc.ParseWithClaims(token, customClaims)
				if err != nil {
					c.Logger().Debugf("token is not valid: %v", err)

					return c.JSON(http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})
				}

				// next middlewares can check roles
				c.Set("claims", customClaims)
				c.Set("provider", jwtToken.Header["provider_name"])
				addXUserHeader(c.Request(), customClaims, m.Provider[jwtToken.Header["provider_name"].(string)].XUser, m.Provider[jwtToken.Header["provider_name"].(string)].EmailVerifyCheck)

				if v, _ := c.Get(CtxTokenHeaderDelKey).(bool); v {
					c.Request().Header.Del("Authorization")
				}

				return next(c)
			}
		}

		// get token from store
		// if not exist, redirect to login page with redirect url
		// set token to the header and continue

		// check if token exist in store
		token64 := ""
		providerName := ""
		if v, err := m.store.Get(c.Request(), m.GetCookieName(c)); !v.IsNew && err == nil {
			// add the access token to the request
			token64, _ = v.Values[TokenKey].(string)
			providerName, _ = v.Values[ProviderKey].(string)
		} else {
			if err != nil {
				c.Logger().Errorf("cannot get session: %v", err)
			}

			// cookie not found, redirect to login page
			return m.RedirectToLogin(c, m.store, true, false)
		}

		// check if token is valid
		token, err := ParseToken64(token64)
		if err != nil {
			c.Logger().Errorf("cannot parse token: %v", err)
			return m.RedirectToLogin(c, m.store, true, true)
		}

		// check if token is expired
		if !m.Action.Token.DisableRefresh {
			v, err := auth.IsRefreshNeed(token.AccessToken)
			if err != nil {
				c.Logger().Errorf("cannot check if token is expired: %v", err)
				return m.RedirectToLogin(c, m.store, true, true)
			}

			if v {
				provider, ok := m.Provider[providerName]
				if !ok || provider.Oauth2 == nil {
					c.Logger().Errorf("cannot find provider %q", providerName)
					return m.RedirectToLogin(c, m.store, true, true)
				}

				requestConfig := request.AuthRequestConfig{
					TokenURL:     provider.Oauth2.TokenURL,
					ClientID:     provider.Oauth2.ClientID,
					ClientSecret: provider.Oauth2.ClientSecret,
				}

				requestConfig.Scopes = strings.Fields(token.Scope)

				refreshData, err := m.Action.Token.auth.RefreshToken(c.Request().Context(), request.RefreshTokenConfig{
					RefreshToken:      token.RefreshToken,
					AuthRequestConfig: requestConfig,
				})
				if err != nil {
					c.Logger().Warnf("cannot refresh token: %v", err)
					return m.RedirectToLogin(c, m.store, true, true)
				}

				// set new token to the store
				if err := m.SetToken(c, refreshData, providerName); err != nil {
					c.Logger().Errorf("cannot set session: %v", err)
					return m.RedirectToLogin(c, m.store, true, true)
				}

				// add the access token to the request
				token, err = ParseToken(refreshData)
				if err != nil {
					c.Logger().Errorf("cannot parse token: %v", err)
					return m.RedirectToLogin(c, m.store, true, true)
				}
			}
		}

		// check if token is valid
		customClaims := &claims.Custom{}
		jwtToken, err := m.Action.Token.keyFunc.ParseWithClaims(token.AccessToken, customClaims)
		if err != nil {
			c.Logger().Debugf("token is not valid: %v", err)
			return m.RedirectToLogin(c, m.store, true, true)
		}

		// next middlewares can check roles
		c.Set("claims", customClaims)
		c.Set("provider", jwtToken.Header["provider_name"])
		addXUserHeader(c.Request(), customClaims, m.Provider[jwtToken.Header["provider_name"].(string)].XUser, m.Provider[jwtToken.Header["provider_name"].(string)].EmailVerifyCheck)

		// add the access token to the request
		if v, _ := c.Get(CtxTokenHeaderKey).(bool); v {
			c.Request().Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
		}

		if v, _ := c.Get(CtxTokenHeaderDelKey).(bool); v {
			c.Request().Header.Del("Authorization")
		}

		return next(c)
	}

	return c.JSON(http.StatusNotFound, MetaData{Error: fmt.Sprintf("action %q not found", m.Action.Active)})
}

func (m *Session) RedirectToLogin(c echo.Context, store StoreInf, addRedirectPath bool, removeSession bool) error {
	// check redirection is disabled
	if v, _ := c.Get(CtxDisableRedirectKey).(bool); v {
		return c.JSON(http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})
	}

	if removeSession {
		if err := m.DelToken(c); err != nil {
			c.Logger().Debugf("cannot remove session: %v", err)
		}
	}

	// add redirect_path query param
	if !addRedirectPath {
		return c.Redirect(http.StatusTemporaryRedirect, m.Action.Token.LoginPath)
	}

	return c.Redirect(http.StatusTemporaryRedirect, loginPathWithRedirect(c, m.Action.Token.LoginPath))
}

func loginPathWithRedirect(c echo.Context, loginPath string) string {
	redirectPath := c.Request().URL.Path
	if c.Request().URL.RawQuery != "" {
		redirectPath = fmt.Sprintf("%s?%s", redirectPath, c.Request().URL.RawQuery)
	}

	if (redirectPath == "" || redirectPath == "/") && c.Request().URL.RawQuery == "" {
		return loginPath
	}

	return fmt.Sprintf("%s?redirect_path=%s", loginPath, url.QueryEscape(redirectPath))
}

func (m *Session) GetToken(c echo.Context) (*TokenData, *Oauth2, error) {
	// check if token exist in store
	v64 := ""
	providerName := ""
	if v, err := m.store.Get(c.Request(), m.GetCookieName(c)); !v.IsNew && err == nil {
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
		c.Logger().Errorf("cannot parse token: %v", err)
		return nil, nil, err
	}

	provider, ok := m.Provider[providerName]
	if !ok || provider.Oauth2 == nil {
		c.Logger().Errorf("cannot find provider %q", providerName)
		return nil, nil, fmt.Errorf("cannot find provider %q", providerName)
	}

	return token, provider.Oauth2, nil
}

// IsLogged check token is exist and valid.
func (m *Session) IsLogged(c echo.Context) (bool, error) {
	// check if token exist in store
	v64 := ""
	providerName := ""
	if v, err := m.store.Get(c.Request(), m.GetCookieName(c)); !v.IsNew && err == nil {
		// add the access token to the request
		v64, _ = v.Values[TokenKey].(string)
		providerName, _ = v.Values[ProviderKey].(string)
	} else {
		if err != nil {
			return false, err
		}

		// cookie not found, redirect to login page
		return false, nil
	}

	// check if token is valid
	token, err := ParseToken64(v64)
	if err != nil {
		c.Logger().Errorf("cannot parse token: %v", err)
		return false, err
	}

	// check if token is expired
	if !m.Action.Token.DisableRefresh {
		v, err := auth.IsRefreshNeed(token.AccessToken)
		if err != nil {
			c.Logger().Errorf("cannot check if token is expired: %v", err)
			return false, err
		}

		if v {
			provider, ok := m.Provider[providerName]
			if !ok || provider.Oauth2 == nil {
				c.Logger().Errorf("cannot find provider %q", providerName)
				return false, fmt.Errorf("cannot find provider %q", providerName)
			}

			requestConfig := request.AuthRequestConfig{
				TokenURL:     provider.Oauth2.TokenURL,
				ClientID:     provider.Oauth2.ClientID,
				ClientSecret: provider.Oauth2.ClientSecret,
			}

			requestConfig.Scopes = strings.Fields(token.Scope)

			refreshData, err := m.Action.Token.auth.RefreshToken(c.Request().Context(), request.RefreshTokenConfig{
				RefreshToken:      token.RefreshToken,
				AuthRequestConfig: requestConfig,
			})
			if err != nil {
				c.Logger().Errorf("cannot refresh token: %v", err)
				return false, err
			}

			// set new token to the store
			if err := m.SetToken(c, refreshData, providerName); err != nil {
				c.Logger().Errorf("cannot set session: %v", err)
				return false, err
			}

			// add the access token to the request
			token, err = ParseToken(refreshData)
			if err != nil {
				c.Logger().Errorf("cannot parse token: %v", err)
				return false, err
			}
		}
	}

	// check if token is valid
	if _, err := m.Action.Token.keyFunc.ParseWithClaims(token.AccessToken, &jwt.RegisteredClaims{}); err != nil {
		c.Logger().Debugf("token is not valid: %v", err)
		return false, err
	}

	return true, nil
}

func (m *Session) RedirectToMain(c echo.Context) error {
	redirectPath := c.Request().URL.Query().Get("redirect_path")
	if redirectPath == "" {
		redirectPath = "/"
	}

	return c.Redirect(http.StatusTemporaryRedirect, redirectPath)
}
