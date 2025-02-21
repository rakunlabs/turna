package login

import (
	"net/http"
	"path"
	"sort"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
)

type Info struct {
	Title string `cfg:"title"`
}

type InfoUIResponse struct {
	Title    string       `json:"title"`
	Provider InfoProvider `json:"provider"`
	Error    string       `json:"error,omitempty"`
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

func (m *Login) InformationUI(w http.ResponseWriter, r *http.Request) {
	info := m.Info.value()

	response := InfoUIResponse{
		Title: info.Title,
	}

	for providerName := range m.session.Provider {
		if m.session.Provider[providerName].Hide {
			continue
		}

		oauth2 := m.session.Provider[providerName].Oauth2
		if oauth2 == nil {
			continue
		}

		name := providerName
		if m.session.Provider[providerName].Name != "" {
			name = m.session.Provider[providerName].Name
		}

		if m.session.Provider[providerName].PasswordFlow {
			response.Provider.Password = append(response.Provider.Password, Link{
				Name:     name,
				URL:      m.Path.BaseURL + path.Join(m.pathFixed.Token, providerName),
				Priority: m.session.Provider[providerName].Priority,
			})

			continue
		}

		response.Provider.Code = append(response.Provider.Code, Link{
			Name:     name,
			URL:      m.Path.BaseURL + path.Join(m.pathFixed.Code, providerName),
			Priority: m.session.Provider[providerName].Priority,
		})
	}

	// sort by priority
	sort.Slice(response.Provider.Code, func(i, j int) bool {
		return response.Provider.Code[i].Priority < response.Provider.Code[j].Priority
	})

	sort.Slice(response.Provider.Password, func(i, j int) bool {
		return response.Provider.Password[i].Priority < response.Provider.Password[j].Priority
	})

	httputil.JSON(w, http.StatusOK, response)
}
