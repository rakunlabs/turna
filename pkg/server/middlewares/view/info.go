package view

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/model"
	"gopkg.in/yaml.v3"
)

type Info struct {
	Swagger         []Swagger       `cfg:"swagger"          json:"swagger"`
	SwaggerSettings SwaggerSettings `cfg:"swagger_settings" json:"swagger_settings"`
}

type SwaggerSettings struct {
	Schemes                []string `cfg:"schemes"                  json:"schemes,omitempty"`
	Host                   string   `cfg:"host"                     json:"host,omitempty"`
	BasePath               string   `cfg:"base_path"                json:"base_path,omitempty"`
	BasePathPrefix         string   `cfg:"base_path_prefix"         json:"base_path_prefix,omitempty"`
	DisableAuthorizeButton bool     `cfg:"disable_authorize_button" json:"disable_authorize_button,omitempty"`
}

type Swagger struct {
	Name                   string   `cfg:"name"                     json:"name,omitempty"`
	Link                   string   `cfg:"link"                     json:"link,omitempty"`
	Schemes                []string `cfg:"schemes"                  json:"schemes,omitempty"`
	Host                   string   `cfg:"host"                     json:"host,omitempty"`
	BasePath               string   `cfg:"base_path"                json:"base_path,omitempty"`
	BasePathPrefix         string   `cfg:"base_path_prefix"         json:"base_path_prefix,omitempty"`
	DisableAuthorizeButton *bool    `cfg:"disable_authorize_button" json:"disable_authorize_button,omitempty"`
}

func (m *View) InformationUI(c echo.Context) error {
	if m.InfoURL == "" {
		return c.JSON(http.StatusOK, m.Info)
	}

	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, m.InfoURL, nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Error: err.Error()})
	}

	var body []byte
	if err := m.client.Do(req, func(r *http.Response) error {
		if r.StatusCode != http.StatusOK {
			return fmt.Errorf("status code: %d", r.StatusCode)
		}

		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Error: err.Error()})
	}

	if strings.ToLower(m.InfoURLType) == "yaml" {
		var info interface{}
		if err := yaml.Unmarshal(body, &info); err != nil {
			return c.JSON(http.StatusNotAcceptable, model.MetaData{Error: err.Error()})
		}

		return c.JSON(http.StatusOK, info)
	}

	return c.JSONBlob(http.StatusOK, body)
}
