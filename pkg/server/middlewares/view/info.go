package view

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Info struct {
	Swagger         map[string]Swagger `cfg:"swagger"`
	SwaggerSettings SwaggerSettings    `cfg:"swagger_settings"`
	SwaggerKV       map[string]string  `cfg:"swagger_kv"`
}

func (i Info) MarshalJSON() ([]byte, error) {
	swagger := make(map[string]Swagger, len(i.Swagger)+len(i.SwaggerKV))
	for k, v := range i.Swagger {
		if v.Schemes == nil {
			v.Schemes = i.SwaggerSettings.Schemes
		}
		if v.Host == "" {
			v.Host = i.SwaggerSettings.Host
		}
		if v.BasePath == "" {
			v.BasePath = i.SwaggerSettings.BasePath
		}
		if v.BasePathPrefix == "" {
			v.BasePathPrefix = i.SwaggerSettings.BasePathPrefix
		}
		if v.DisableAuthorizeButton == false {
			v.DisableAuthorizeButton = i.SwaggerSettings.DisableAuthorizeButton
		}

		swagger[k] = v
	}

	for k, v := range i.SwaggerKV {
		swagger[k] = Swagger{
			Link:                   v,
			Host:                   i.SwaggerSettings.Host,
			BasePath:               i.SwaggerSettings.BasePath,
			BasePathPrefix:         i.SwaggerSettings.BasePathPrefix,
			DisableAuthorizeButton: i.SwaggerSettings.DisableAuthorizeButton,
		}
	}

	v := struct {
		Swagger map[string]Swagger `json:"swagger"`
	}{
		Swagger: swagger,
	}

	return json.Marshal(v)
}

type SwaggerSettings struct {
	Schemes                []string `cfg:"schemes" json:"schemes"`
	Host                   string   `cfg:"host" json:"host"`
	BasePath               string   `cfg:"base_path" json:"basePath"`
	BasePathPrefix         string   `cfg:"base_path_prefix" json:"basePathPrefix"`
	DisableAuthorizeButton bool     `cfg:"disable_authorize_button" json:"disableAuthorizeButton"`
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
	return c.JSON(http.StatusOK, m.Info)
}
