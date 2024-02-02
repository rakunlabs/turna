package login

import (
	"net/http"
	"path"
	"sort"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/middlewares/session"
	"github.com/worldline-go/turna/pkg/server/model"
)

type Info struct {
	Title string `cfg:"title"`
}

type InfoUIResponse struct {
	Title    string       `json:"title"`
	Provider InfoProvider `json:"provider"`
}

type InfoProvider struct {
	Password []Link `json:"password"`
	Code     []Link `json:"code"`
}

type Link struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Priority int    `json:"-"`
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
		Title: info.Title,
	}

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Error: "session middleware not found"})
	}

	for providerName := range sessionM.Provider {
		oauth2 := sessionM.Provider[providerName].Oauth2
		if oauth2 == nil {
			continue
		}

		if sessionM.Provider[providerName].PasswordFlow {
			response.Provider.Password = append(response.Provider.Password, Link{
				Name:     providerName,
				URL:      m.Path.BaseURL + path.Join(m.pathFixed.Token, providerName),
				Priority: sessionM.Provider[providerName].Priority,
			})

			continue
		}

		response.Provider.Code = append(response.Provider.Code, Link{
			Name:     providerName,
			URL:      m.Path.BaseURL + path.Join(m.pathFixed.Code, providerName),
			Priority: sessionM.Provider[providerName].Priority,
		})
	}

	// sort by priority
	sort.Slice(response.Provider.Code, func(i, j int) bool {
		return response.Provider.Code[i].Priority < response.Provider.Code[j].Priority
	})

	sort.Slice(response.Provider.Password, func(i, j int) bool {
		return response.Provider.Password[i].Priority < response.Provider.Password[j].Priority
	})

	return c.JSON(http.StatusOK, response)
}
