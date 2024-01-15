package login

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/middlewares/session"
)

type Info struct {
	Title string `cfg:"title"`
}

type InfoUIResponse struct {
	Title     string   `json:"title"`
	Providers []string `json:"providers"`
}

func (i Info) value() Info {
	if i.Title == "" {
		i.Title = "Login"
	}

	return i
}

func (m *Login) InformationUI(c echo.Context) error {
	info := m.Info.value()

	response := InfoUIResponse{
		Title:     info.Title,
		Providers: make([]string, 0, len(m.Provider)),
	}

	for providerName := range m.Provider {
		response.Providers = append(response.Providers, providerName)
	}

	return c.JSON(http.StatusOK, response)
}

func (m *Login) InformationToken(c echo.Context) error {
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, MetaData{Error: "session middleware not found"})
	}

	return sessionM.Info(c)
}
