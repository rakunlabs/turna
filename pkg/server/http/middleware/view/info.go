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

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/model"
	"gopkg.in/yaml.v3"
)

type Info struct {
	Iframe          []Iframe        `cfg:"iframe"           json:"iframe"`
	Page            []Page          `cfg:"page"             json:"page"`
	Grpc            []Grpc          `cfg:"grpc"             json:"grpc"`
	Swagger         []Swagger       `cfg:"swagger"          json:"swagger"`
	SwaggerSettings SwaggerSettings `cfg:"swagger_settings" json:"swagger_settings"`
}

type Iframe struct {
	Name string `cfg:"name" json:"name,omitempty"`
	Path string `cfg:"path" json:"path,omitempty"`
	URL  string `cfg:"url"  json:"url,omitempty"`
}

// Page for iframe view.
type Page struct {
	Name   string       `cfg:"name"   json:"name,omitempty"`
	Path   string       `cfg:"path"   json:"path,omitempty"`
	URL    string       `cfg:"url"    json:"-"`
	Header HeaderHolder `cfg:"header" json:"-"`
	Host   bool         `cfg:"host"   json:"-"`
}

type HeaderHolder struct {
	Request  Header `cfg:"request"`
	Response Header `cfg:"response"`
}

type Header struct {
	AddHeader    map[string]string `cfg:"add_header"`
	RemoveHeader []string          `cfg:"remove_header"`
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

func (m *View) InformationUI(w http.ResponseWriter, r *http.Request) {
	if m.InfoURL == "" {
		httputil.JSON(w, http.StatusOK, m.Info)

		return
	}

	body, err := m.infoRequest(r.Context(), true)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

		return
	}

	if strings.ToLower(m.InfoURLType) == "yaml" {
		var info interface{}
		if err := yaml.Unmarshal(body, &info); err != nil {
			httputil.JSON(w, http.StatusNotAcceptable, model.MetaData{Message: err.Error()})

			return
		}

		httputil.JSON(w, http.StatusOK, body)

		return
	}

	httputil.JSONBlob(w, http.StatusOK, body)
}
