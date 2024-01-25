package view

import "github.com/labstack/echo/v4"

type Info struct {
	Swagger map[string]Swagger `cfg:"swagger" json:"swagger"`
}

type Swagger struct {
	Link                   string   `cfg:"link" json:"link"`
	Schemes                []string `cfg:"schemes" json:"schemes"`
	Host                   string   `cfg:"host" json:"host"`
	BasePath               string   `cfg:"base_path" json:"basePath"`
	BasePathPrefix         string   `cfg:"base_path_prefix" json:"basePathPrefix"`
	DisableAuthorizeButton bool     `cfg:"disable_authorize_button" json:"disableAuthorizeButton"`
}

func (m *View) InformationUI(c echo.Context) error {
	return c.JSON(200, m.Info)
}
