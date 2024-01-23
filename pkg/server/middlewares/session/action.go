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
	storeAuth "github.com/worldline-go/auth/store"
	"github.com/worldline-go/klient"
)

const actionToken = "token"

type Actions struct {
	Active string `cfg:"active"`
	Token  *Token `cfg:"token"`
}

type Token struct {
	LoginPath          string       `cfg:"login_path"`
	DisableRefresh     bool         `cfg:"disable_refresh"`
	Oauth2             Oauth2Config `cfg:"oauth2"`
	InsecureSkipVerify bool         `cfg:"insecure_skip_verify"`

	auth    request.Auth            `cfg:"-"`
	keyFunc models.InfKeyFuncParser `cfg:"-"`
}

type Oauth2Config struct {
	TokenURL     string `cfg:"token_url"`
	CertURL      string `cfg:"cert_url"`
	ClientID     string `cfg:"client_id"`
	ClientSecret string `cfg:"client_secret"`
}

func (m *Session) SetAction() error {
	if m.Actions.Token != nil {
		// set auth client
		client, err := klient.New(
			klient.WithDisableBaseURLCheck(true),
			klient.WithDisableRetry(true),
			klient.WithDisableEnvValues(true),
			klient.WithInsecureSkipVerify(m.Actions.Token.InsecureSkipVerify),
		)
		if err != nil {
			return fmt.Errorf("cannot create klient: %w", err)
		}

		m.Actions.Token.auth.Client = client.HTTP

		provider := auth.ProviderExtra{
			InfProvider: &providers.Generic{
				ClientID:     m.Actions.Token.Oauth2.ClientID,
				ClientSecret: m.Actions.Token.Oauth2.ClientSecret,
				CertURL:      m.Actions.Token.Oauth2.CertURL,
			},
		}

		jwks, err := provider.JWTKeyFunc()
		if err != nil {
			return fmt.Errorf("cannot create keyfunc: %w", err)
		}

		m.Actions.Token.keyFunc = jwks
	}

	// set active action
	if m.Actions.Active != "" {
		return nil
	}

	if m.Actions.Token != nil {
		m.Actions.Active = actionToken

		return nil
	}

	return nil
}

func (m *Session) Do(next echo.HandlerFunc, c echo.Context) error {
	if m.Actions.Active == actionToken {
		// get token from store
		// if not exist, redirect to login page with redirect url
		// set token to the header and continue

		// check if token exist in store
		v64 := ""
		if v, err := m.store.Get(c.Request(), m.CookieName); !v.IsNew && err == nil {
			// add the access token to the request
			v64, _ = v.Values[m.ValueName].(string)
		} else {
			if err != nil {
				c.Logger().Errorf("cannot get session: %v", err)
			}

			// cookie not found, redirect to login page
			return m.RedirectToLogin(c, m.store, true, false)
		}

		// check if token is valid
		token, err := storeAuth.Parse(v64, storeAuth.WithBase64(true))
		if err != nil {
			c.Logger().Errorf("cannot parse token: %v", err)
			return m.RedirectToLogin(c, m.store, true, true)
		}

		// check if token is expired
		if !m.Actions.Token.DisableRefresh {
			v, err := auth.IsRefreshNeed(token.AccessToken)
			if err != nil {
				c.Logger().Errorf("cannot check if token is expired: %v", err)
				return m.RedirectToLogin(c, m.store, true, true)
			}

			if v {
				requestConfig := request.AuthRequestConfig{
					TokenURL:     m.Actions.Token.Oauth2.TokenURL,
					ClientID:     m.Actions.Token.Oauth2.ClientID,
					ClientSecret: m.Actions.Token.Oauth2.ClientSecret,
				}

				requestConfig.Scopes = strings.Fields(token.Scope)

				refreshData, err := m.Actions.Token.auth.RefreshToken(c.Request().Context(), request.RefreshTokenConfig{
					RefreshToken:      token.RefreshToken,
					AuthRequestConfig: requestConfig,
				})
				if err != nil {
					c.Logger().Errorf("cannot refresh token: %v", err)
					return m.RedirectToLogin(c, m.store, true, true)
				}

				// set new token to the store
				if _, err := SetSessionB64(c.Request(), c.Response(), refreshData, m.CookieName, m.ValueName, m.store); err != nil {
					c.Logger().Errorf("cannot set session: %v", err)
					return m.RedirectToLogin(c, m.store, true, true)
				}

				// add the access token to the request
				token, err = storeAuth.Parse(string(refreshData))
				if err != nil {
					c.Logger().Errorf("cannot parse token: %v", err)
					return m.RedirectToLogin(c, m.store, true, true)
				}
			}
		}

		// check if token is valid
		customClaims := &claims.Custom{}
		if _, err := m.Actions.Token.keyFunc.ParseWithClaims(token.AccessToken, customClaims); err != nil {
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

	return c.JSON(http.StatusNotFound, MetaData{Error: fmt.Sprintf("action %q not found", m.Actions.Active)})
}

func (m *Session) RedirectToLogin(c echo.Context, store StoreInf, addRedirectPath bool, removeSession bool) error {
	// check redirection is disabled
	if v, _ := c.Get(CtxDisableRedirectKey).(bool); v {
		return c.JSON(http.StatusProxyAuthRequired, MetaData{Error: http.StatusText(http.StatusProxyAuthRequired)})
	}

	if removeSession {
		err := RemoveSession(c.Request(), c.Response(), m.CookieName, store)
		if err != nil {
			c.Logger().Debugf("cannot remove session: %v", err)
		}
	}

	// add redirect_path query param
	if !addRedirectPath {
		return c.Redirect(http.StatusTemporaryRedirect, m.Actions.Token.LoginPath)
	}

	return c.Redirect(http.StatusTemporaryRedirect, loginPathWithRedirect(c, m.Actions.Token.LoginPath))
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
	if v, err := m.store.Get(c.Request(), m.CookieName); !v.IsNew && err == nil {
		// add the access token to the request
		v64, _ = v.Values[m.ValueName].(string)
	} else {
		if err != nil {
			return false, err
		}

		// cookie not found, redirect to login page
		return false, nil
	}

	// check if token is valid
	token, err := storeAuth.Parse(v64, storeAuth.WithBase64(true))
	if err != nil {
		c.Logger().Errorf("cannot parse token: %v", err)
		return false, err
	}

	// check if token is expired
	if !m.Actions.Token.DisableRefresh {
		v, err := auth.IsRefreshNeed(token.AccessToken)
		if err != nil {
			c.Logger().Errorf("cannot check if token is expired: %v", err)
			return false, err
		}

		if v {
			requestConfig := request.AuthRequestConfig{
				TokenURL:     m.Actions.Token.Oauth2.TokenURL,
				ClientID:     m.Actions.Token.Oauth2.ClientID,
				ClientSecret: m.Actions.Token.Oauth2.ClientSecret,
			}

			requestConfig.Scopes = strings.Fields(token.Scope)

			refreshData, err := m.Actions.Token.auth.RefreshToken(c.Request().Context(), request.RefreshTokenConfig{
				RefreshToken:      token.RefreshToken,
				AuthRequestConfig: requestConfig,
			})
			if err != nil {
				c.Logger().Errorf("cannot refresh token: %v", err)
				return false, err
			}

			// set new token to the store
			if _, err := SetSessionB64(c.Request(), c.Response(), refreshData, m.CookieName, m.ValueName, m.store); err != nil {
				c.Logger().Errorf("cannot set session: %v", err)
				return false, err
			}

			// add the access token to the request
			token, err = storeAuth.Parse(string(refreshData))
			if err != nil {
				c.Logger().Errorf("cannot parse token: %v", err)
				return false, err
			}
		}
	}

	// check if token is valid
	if _, err := m.Actions.Token.keyFunc.ParseWithClaims(token.AccessToken, &jwt.RegisteredClaims{}); err != nil {
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
