package view

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type Info struct {
	Swagger         []Swagger       `cfg:"swagger" json:"swagger"`
	SwaggerSettings SwaggerSettings `cfg:"swagger_settings" json:"swagger_settings"`
}

type SwaggerSettings struct {
	Schemes                []string `cfg:"schemes"                  json:"schemes,omitempty"`
	Host                   string   `cfg:"host"                     json:"host,omitempty"`
	BasePath               string   `cfg:"base_path"                json:"basePath,omitempty"`
	BasePathPrefix         string   `cfg:"base_path_prefix"         json:"basePathPrefix,omitempty"`
	DisableAuthorizeButton bool     `cfg:"disable_authorize_button" json:"disableAuthorizeButton,omitempty"`
}

type Swagger struct {
	Name                   string   `cfg:"name"                     json:"name,omitempty"`
	Link                   string   `cfg:"link"                     json:"link,omitempty"`
	Schemes                []string `cfg:"schemes"                  json:"schemes,omitempty"`
	Host                   string   `cfg:"host"                     json:"host,omitempty"`
	BasePath               string   `cfg:"base_path"                json:"basePath,omitempty"`
	BasePathPrefix         string   `cfg:"base_path_prefix"         json:"basePathPrefix,omitempty"`
	DisableAuthorizeButton *bool    `cfg:"disable_authorize_button" json:"disableAuthorizeButton"`
}

func (m *View) InformationUI(c echo.Context) error {
	return c.JSON(http.StatusOK, m.Info)
}
