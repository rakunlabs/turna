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
	"github.com/worldline-go/auth/models"
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
	LoginPath          string              `cfg:"login_path"`
	DisableRefresh     bool                `cfg:"disable_refresh"`
	Provider           map[string]Provider `cfg:"provider"`
	InsecureSkipVerify bool                `cfg:"insecure_skip_verify"`

	auth    request.Auth            `cfg:"-"`
	keyFunc models.InfKeyFuncParser `cfg:"-"`
}

type Provider struct {
	Oauth2 *Oauth2Config `cfg:"oauth2"`
}

type Oauth2Config struct {
	TokenURL     string `cfg:"token_url"`
	CertURL      string `cfg:"cert_url"`
	ClientID     string `cfg:"client_id"`
	ClientSecret string `cfg:"client_secret"`
}

type ProviderWrapper struct {
	Generic *providers.Generic
}

func (p *ProviderWrapper) GetCertURL() string {
	return p.Generic.CertURL
}

func (p *ProviderWrapper) IsNoop() bool {
	return false
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

		providerList := make([]auth.InfProviderCert, 0, len(m.Action.Token.Provider))
		for _, v := range m.Action.Token.Provider {
			if v.Oauth2 == nil {
				continue
			}

			providerList = append(providerList, &ProviderWrapper{
				Generic: &providers.Generic{
					CertURL: v.Oauth2.CertURL,
				},
			})
		}

		jwksMulti, err := auth.MultiJWTKeyFunc(providerList)
		if err != nil {
			return fmt.Errorf("cannot create keyfunc: %w", err)
		}

		m.Action.Token.keyFunc = jwksMulti.(models.InfKeyFuncParser)
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

func (m *Session) Do(next echo.HandlerFunc, c echo.Context) error {
	if m.Action.Active == actionToken {
		// get token from store
		// if not exist, redirect to login page with redirect url
		// set token to the header and continue

		// check if token exist in store
		token64 := ""
		providerName := ""
		if v, err := m.store.Get(c.Request(), m.CookieName); !v.IsNew && err == nil {
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
				provider, ok := m.Action.Token.Provider[providerName]
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
					c.Logger().Errorf("cannot refresh token: %v", err)
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
		if _, err := m.Action.Token.keyFunc.ParseWithClaims(token.AccessToken, customClaims); err != nil {
			c.Logger().Debugf("token is not valid: %v", err)
			return m.RedirectToLogin(c, m.store, true, true)
		}

		// next middlewares can check roles
		c.Set("claims", customClaims)

		// add the access token to the request
		if v, _ := c.Get(CtxTokenHeaderKey).(bool); v {
			c.Request().Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
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

	return fmt.Sprintf("%s?redirect_path=%s", loginPath, url.QueryEscape(redirectPath))
}

// IsLogged check token is exist and valid.
func (m *Session) IsLogged(c echo.Context) (bool, error) {
	// check if token exist in store
	v64 := ""
	providerName := ""
	if v, err := m.store.Get(c.Request(), m.CookieName); !v.IsNew && err == nil {
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
		fmt.Printf("%#v\n", token)
		v, err := auth.IsRefreshNeed(token.AccessToken)
		if err != nil {
			c.Logger().Errorf("cannot check if token is expired: %v", err)
			return false, err
		}

		if v {
			provider, ok := m.Action.Token.Provider[providerName]
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
