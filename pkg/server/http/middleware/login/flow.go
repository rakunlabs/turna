package login

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/rakunlabs/turna/pkg/server/model"
)

type TokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (m *Login) CodeFlowInit(c echo.Context, providerName string) error {
	m.RemoveSuccess(c)

	state, err := NewState()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: err.Error()})
	}

	m.SetState(c, state)

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: "session middleware not found"})
	}

	provider := sessionM.Provider[providerName]
	if provider.Oauth2 == nil {
		return c.JSON(http.StatusNotFound, model.MetaData{Message: fmt.Sprintf("provider %q not found", providerName)})
	}

	authCodeURL, err := m.AuthCodeURL(c.Request(), state, providerName, provider.Oauth2)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: err.Error()})
	}

	return c.Redirect(http.StatusTemporaryRedirect, authCodeURL)
}

func (m *Login) CodeFlow(c echo.Context) error {
	pathSplit := strings.Split(c.Request().URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: "session middleware not found"})
	}

	var oauth2 *session.Oauth2
	if v, ok := sessionM.Provider[providerName]; ok && v.Oauth2 != nil {
		oauth2 = v.Oauth2
	}

	if oauth2 == nil {
		return c.JSON(http.StatusNotFound, model.MetaData{Message: fmt.Sprintf("provider %q not found", providerName)})
	}

	// get the token from the request
	code := c.QueryParam("code")
	if code == "" {
		return m.CodeFlowInit(c, providerName)
	}

	state := c.QueryParam("state")
	// check state
	if m.CheckState(c, state) != nil {
		return c.JSON(http.StatusUnauthorized, model.MetaData{Message: "state is not valid"})
	}

	// get token from provider
	data, statusCode, err := m.CodeToken(c, code, providerName, oauth2)
	if err != nil {
		return c.JSON(statusCode, model.MetaData{Message: err.Error()})
	}

	if err := sessionM.SetToken(c, data, providerName); err != nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: err.Error()})
	}

	// pop-up close window
	m.SetSuccess(c, "true")
	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response().Write([]byte("<script>window.close();</script>"))

	return nil
}

func (m *Login) PasswordFlow(c echo.Context) error {
	pathSplit := strings.Split(c.Request().URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	// save token in session
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: "session middleware not found"})
	}

	var oauth2 *session.Oauth2
	if v, ok := sessionM.Provider[providerName]; ok && v.Oauth2 != nil {
		oauth2 = v.Oauth2
	}

	if oauth2 == nil {
		return c.JSON(http.StatusNotFound, model.MetaData{Message: fmt.Sprintf("provider %q not found", providerName)})
	}

	var request TokenRequest

	if err := c.Bind(&request); err != nil {
		return err
	}

	// get Token from provider
	data, statusCode, err := m.PasswordToken(c.Request().Context(), request.Username, request.Password, oauth2)
	if err != nil {
		return c.JSON(statusCode, model.MetaData{Message: err.Error()})
	}

	if err := sessionM.SetToken(c, data, providerName); err != nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}
