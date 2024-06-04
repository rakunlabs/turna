package login

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/rakunlabs/turna/pkg/server/model"
	"golang.org/x/exp/slog"
)

func (m *Login) Logout(c echo.Context) error {
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: "session middleware not found"})
	}

	token, oauth2, err := sessionM.GetToken(c)
	if err == nil && oauth2.LogoutURL != "" {
		if token.IDToken == "" {
			slog.Error("id_token is empty")
		} else {
			logoutURL, err := url.Parse(oauth2.LogoutURL)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, model.MetaData{Message: "failed to parse logout URL"})
			}

			q := logoutURL.Query()
			q.Set("id_token_hint", token.IDToken)
			q.Set("client_id", oauth2.ClientID)
			logoutURL.RawQuery = q.Encode()

			req := &http.Request{
				Method: http.MethodGet,
				URL:    logoutURL,
			}

			if err := m.client.Do(req, func(resp *http.Response) error {
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					return fmt.Errorf("failed to logout: %s", resp.Status)
				}

				return nil
			}); err != nil {
				slog.Error("failed to logout", "err", err.Error())
			}
		}
	}

	return sessionM.RedirectToLogin(c, sessionM.GetStore(), false, true)
}
