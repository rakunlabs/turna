package login

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/request"
	"github.com/worldline-go/klient"
	"github.com/worldline-go/turna/pkg/server/middlewares/session"
)

// Login middleware gives a login page.
type Login struct {
	Provider          map[string]Provider `cfg:"provider"`
	DefaultProvider   string              `cfg:"default_provider"`
	UI                UI                  `cfg:"ui"`
	Info              Info                `cfg:"info"`
	Request           Request             `cfg:"request"`
	SessionMiddleware string              `cfg:"session_middleware"`

	auth request.Auth `cfg:"-"`
}

type Request struct {
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
}

type UI struct {
	ExternalFolder bool `cfg:"external_folder"`
	// EmbedPathPrefix is removing prefix path to access to the embedded UI.
	//  - /login/
	EmbedPathPrefix string           `cfg:"embed_path_prefix"`
	embedUIFunc     echo.HandlerFunc `cfg:"-"`
}

type Provider struct {
	Oauth2 *Oauth2 `cfg:"oauth2"`
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

	m.auth.Client = client.HTTP

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if isLogout, _ := c.Get("logout").(bool); isLogout {
				sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
				if sessionM == nil {
					return c.JSON(http.StatusInternalServerError, MetaData{Error: "session middleware not found"})
				}

				return sessionM.RedirectToLogin(c, sessionM.GetStore(), false, true)
			}

			urlPath := c.Request().URL.Path
			method := c.Request().Method

			switch method {
			case http.MethodGet:
				if strings.HasSuffix(urlPath, "/api/v1/info/ui") {
					return m.InformationUI(c)
				}

				if strings.HasSuffix(urlPath, "/api/v1/info/token") {
					return m.InformationToken(c)
				}

				// check to redirection
				sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
				if sessionM == nil {
					return c.JSON(http.StatusInternalServerError, MetaData{Error: "session middleware not found"})
				}
				isLogged, err := sessionM.IsLogged(c)
				if isLogged {
					return sessionM.RedirectToMain(c)
				}
				if err != nil {
					_ = session.RemoveSession(c.Request(), c.Response(), sessionM.CookieName, sessionM.GetStore())
				}

				if m.UI.ExternalFolder {
					return next(c)
				}

				return m.View(c)
			case http.MethodPost:
				if strings.HasSuffix(urlPath, "/api/v1/token") {
					return m.Token(c)
				}
			}

			// not found
			return c.String(http.StatusNotFound, http.StatusText(http.StatusNotFound))
		}
	}, nil
}
