package login

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/middlewares/session"
)

type TokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (m *Login) CodeFlowInit(c echo.Context, providerName string) error {
	state, err := NewState()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: err.Error()})
	}

	m.SetState(c, state)

	oauth2 := m.Provider[providerName]
	if oauth2.Oauth2 == nil {
		return c.JSON(http.StatusNotFound, MetaData{Error: fmt.Sprintf("provider %q not found", providerName)})
	}

	authCodeURL, err := m.AuthCodeURL(c.Request(), state, providerName)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: err.Error()})
	}

	return c.Redirect(http.StatusTemporaryRedirect, authCodeURL)
}

func (m *Login) CodeFlow(c echo.Context) error {
	pathSplit := strings.Split(c.Request().URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	var oauth2 *Oauth2
	if v, ok := m.Provider[providerName]; ok && v.Oauth2 != nil {
		oauth2 = v.Oauth2
	}

	if oauth2 == nil {
		return c.JSON(http.StatusNotFound, MetaData{Error: fmt.Sprintf("provider %q not found", providerName)})
	}

	// get the token from the request
	code := c.QueryParam("code")
	if code == "" {
		return m.CodeFlowInit(c, providerName)
	}

	state := c.QueryParam("state")
	// check state
	if m.CheckState(c, state) != nil {
		return c.JSON(http.StatusUnauthorized, MetaData{Error: "state is not valid"})
	}

	// get token from provider
	data, statusCode, err := m.CodeToken(c, code, providerName, oauth2)
	if err != nil {
		return c.JSON(statusCode, MetaData{Error: err.Error()})
	}

	// save token in session
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: "session middleware not found"})
	}

	if err := sessionM.SetToken(c, data, providerName); err != nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: err.Error()})
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

	var oauth2 *Oauth2
	if v, ok := m.Provider[providerName]; ok && v.Oauth2 != nil {
		oauth2 = v.Oauth2
	}

	if oauth2 == nil {
		return c.JSON(http.StatusNotFound, MetaData{Error: fmt.Sprintf("provider %q not found", providerName)})
	}

	var request TokenRequest

	if err := c.Bind(&request); err != nil {
		return err
	}

	// get Token from provider
	data, statusCode, err := m.PasswordToken(c.Request().Context(), request.Username, request.Password, oauth2)
	if err != nil {
		return c.JSON(statusCode, MetaData{Error: err.Error()})
	}

	// save token in session
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: "session middleware not found"})
	}

	if err := sessionM.SetToken(c, data, providerName); err != nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}
