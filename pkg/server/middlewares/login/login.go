package login

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/klient"
	"github.com/worldline-go/turna/pkg/server/middlewares/session"
	"github.com/worldline-go/turna/pkg/server/model"
)

// Login middleware gives a login page.
type Login struct {
	Path              Path     `cfg:"path"`
	Redirect          Redirect `cfg:"redirect"`
	UI                UI       `cfg:"ui"`
	Info              Info     `cfg:"info"`
	Request           Request  `cfg:"request"`
	SessionMiddleware string   `cfg:"session_middleware"`
	StateCookie       Cookie   `cfg:"state_cookie"`
	SuccessCookie     Cookie   `cfg:"success_cookie"`

	client    *klient.Client `cfg:"-"`
	pathFixed PathFixed      `cfg:"-"`
}

type Path struct {
	Base string `cfg:"base"`
	// BaseURL for adding prefix like https://example.com
	BaseURL string `cfg:"base_url"`

	Code   string `cfg:"code"`
	Token  string `cfg:"token"`
	InfoUI string `cfg:"info_ui"`
}

type PathFixed struct {
	Code   string
	InfoUI string
	Token  string
}

type Request struct {
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
}

type UI struct {
	ExternalFolder bool             `cfg:"external_folder"`
	embedUIFunc    echo.HandlerFunc `cfg:"-"`
}

type Provider struct {
	Oauth2 *session.Oauth2 `cfg:"oauth2"`

	// PasswordFlow is use password flow to get token.
	PasswordFlow bool `cfg:"password_flow"`
	// Priority is use to sort provider.
	Priority int `cfg:"priority"`
}

func (m *Login) Middleware(ctx context.Context, _ string) (echo.MiddlewareFunc, error) {
	if m.SessionMiddleware == "" {
		return nil, fmt.Errorf("session middleware is not set")
	}

	embedUIFunc, err := m.SetView()
	if err != nil {
		return nil, err
	}

	m.UI.embedUIFunc = embedUIFunc(nil)

	// set auth client
	client, err := klient.New(
		klient.WithDisableBaseURLCheck(true),
		klient.WithDisableRetry(true),
		klient.WithDisableEnvValues(true),
		klient.WithInsecureSkipVerify(m.Request.InsecureSkipVerify),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create klient: %w", err)
	}

	m.client = client

	// path settings
	m.Path.Base = path.Join("/", strings.TrimSuffix(m.Path.Base, "/"))

	if m.Path.Code != "" {
		m.pathFixed.Code = m.Path.Code
	} else {
		m.pathFixed.Code = path.Join(m.Path.Base, "auth/code")
	}
	if m.Path.Token != "" {
		m.pathFixed.Token = m.Path.Token
	} else {
		m.pathFixed.Token = path.Join(m.Path.Base, "auth/token")
	}

	if m.Path.InfoUI != "" {
		m.pathFixed.InfoUI = m.Path.InfoUI
	} else {
		m.pathFixed.InfoUI = path.Join(m.Path.Base, "auth/info/ui")
	}

	// state cookie settings
	if m.StateCookie.CookieName == "" {
		m.StateCookie.CookieName = "auth_state"
	}

	if m.StateCookie.MaxAge == 0 {
		m.StateCookie.MaxAge = 360
	}

	if m.StateCookie.Path == "" {
		m.StateCookie.Path = "/"
	}

	if m.StateCookie.SameSite == 0 {
		m.StateCookie.SameSite = http.SameSiteLaxMode
	}

	// success cookie settings
	m.SuccessCookie.CookieName = "auth_verify"

	if m.SuccessCookie.MaxAge == 0 {
		m.SuccessCookie.MaxAge = 60
	}

	if m.SuccessCookie.Path == "" {
		m.SuccessCookie.Path = "/"
	}

	if m.SuccessCookie.SameSite == 0 {
		m.SuccessCookie.SameSite = http.SameSiteLaxMode
	}

	m.SuccessCookie.HttpOnly = false

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if isLogout, _ := c.Get("logout").(bool); isLogout {
				sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
				if sessionM == nil {
					return c.JSON(http.StatusInternalServerError, model.MetaData{Error: "session middleware not found"})
				}

				return sessionM.RedirectToLogin(c, sessionM.GetStore(), false, true)
			}

			urlPath := c.Request().URL.Path
			method := c.Request().Method

			switch method {
			case http.MethodGet:
				if strings.HasPrefix(urlPath, m.pathFixed.Code) {
					return m.CodeFlow(c)
				}

				if strings.HasPrefix(urlPath, m.pathFixed.InfoUI) {
					return m.InformationUI(c)
				}

				// check to redirection
				sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
				if sessionM == nil {
					return c.JSON(http.StatusInternalServerError, model.MetaData{Error: "session middleware not found"})
				}
				isLogged, err := sessionM.IsLogged(c)
				if isLogged {
					return sessionM.RedirectToMain(c)
				}
				if err != nil {
					_ = sessionM.DelToken(c)
				}

				if authInfo, _ := strconv.ParseBool(c.QueryParam("auth_info")); authInfo {
					return m.InformationUI(c)
				}

				m.RemoveSuccess(c)

				if m.UI.ExternalFolder {
					return next(c)
				}

				return m.View(c)
			case http.MethodPost:
				if strings.HasPrefix(urlPath, m.pathFixed.Token) {
					return m.PasswordFlow(c)
				}
			}

			// not found
			return c.String(http.StatusNotFound, http.StatusText(http.StatusNotFound))
		}
	}, nil
}
