package login

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/middlewares/session"
)

type TokenRequest struct {
	Provider string `json:"provider"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (m *Login) Token(c echo.Context) error {
	var request TokenRequest

	if err := c.Bind(&request); err != nil {
		return err
	}

	providerName := request.Provider
	if providerName == "" {
		providerName = m.DefaultProvider
	}

	if providerName == "" {
		if len(m.Provider) == 1 {
			for k := range m.Provider {
				providerName = k
			}
		}
	}

	provider, _ := m.Provider[providerName]
	if provider.Oauth2 == nil {
		return c.JSON(http.StatusNotFound, MetaData{Error: fmt.Sprintf("provider %q not found", providerName)})
	}

	// get Token from provider
	tokenConfig := provider.Oauth2.PasswordToken(request.Username, request.Password)
	data, err := m.auth.Password(c.Request().Context(), tokenConfig)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, MetaData{Error: err.Error()})
	}

	// save token in session
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: "session middleware not found"})
	}

	if _, err := session.SetSessionB64(c.Request(), c.Response(), data, sessionM.CookieName, sessionM.ValueName, sessionM.GetStore()); err != nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}
