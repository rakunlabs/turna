package view

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rakunlabs/turna/pkg/server/model"
	"gopkg.in/yaml.v3"
)

type Info struct {
	Grpc            []Grpc          `cfg:"grpc"             json:"grpc"`
	Swagger         []Swagger       `cfg:"swagger"          json:"swagger"`
	SwaggerSettings SwaggerSettings `cfg:"swagger_settings" json:"swagger_settings"`
}

type Grpc struct {
	Name string `cfg:"name" json:"name,omitempty"`
	Addr string `cfg:"addr" json:"addr,omitempty"`
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

func (m *View) GetInfo(ctx context.Context) (Info, error) {
	if m.InfoURL == "" {
		return m.Info, nil
	}

	body, err := m.infoRequest(ctx, true)
	if err != nil {
		return Info{}, err
	}

	var info Info
	if strings.ToLower(m.InfoURLType) == "yaml" {
		if err := yaml.Unmarshal(body, &info); err != nil {
			return Info{}, err
		}
	} else {
		if err := json.Unmarshal(body, &info); err != nil {
			return Info{}, err
		}
	}

	return info, nil
}

func (m *View) infoRequest(ctx context.Context, cached bool) ([]byte, error) {
	m.infoURLMutex.Lock()
	defer m.infoURLMutex.Unlock()

	if m.infoURLTime.After(time.Now().Add(-DefaultInfoDelay)) {
		return m.infoTmpBody, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.InfoURL, nil)
	if err != nil {
		if cached && m.infoTmpBody != nil {
			slog.Error("failed to create request", "error", err.Error())

			return m.infoTmpBody, nil
		}
		return nil, err
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
		if cached && m.infoTmpBody != nil {
			slog.Error("failed to do request", "error", err.Error())

			return m.infoTmpBody, nil
		}
		return nil, err
	}

	m.infoTmpBody = body
	m.infoURLTime = time.Now()

	return body, nil
}

func (m *View) InformationUI(c echo.Context) error {
	if m.InfoURL == "" {
		return c.JSON(http.StatusOK, m.Info)
	}

	body, err := m.infoRequest(c.Request().Context(), true)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Message: err.Error()})
	}

	if strings.ToLower(m.InfoURLType) == "yaml" {
		var info interface{}
		if err := yaml.Unmarshal(body, &info); err != nil {
			return c.JSON(http.StatusNotAcceptable, model.MetaData{Message: err.Error()})
		}

		return c.JSON(http.StatusOK, info)
	}

	return c.JSONBlob(http.StatusOK, body)
}
