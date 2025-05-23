package view

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/model"
)

type Info struct {
	Home Home `cfg:"home" json:"home"`

	Iframe          []Iframe         `cfg:"iframe"           json:"iframe,omitempty"`
	Page            []Page           `cfg:"page"             json:"page,omitempty"`
	Grpc            []Grpc           `cfg:"grpc"             json:"grpc,omitempty"`
	Swagger         []Swagger        `cfg:"swagger"          json:"swagger,omitempty"`
	SwaggerSettings *SwaggerSettings `cfg:"swagger_settings" json:"swagger_settings,omitempty"`

	Groups []Group `cfg:"groups" json:"groups,omitempty"`
}

type Service struct {
	Name string `cfg:"name" json:"name"`

	Iframe          []Iframe         `cfg:"iframe"           json:"iframe,omitempty"`
	Page            []Page           `cfg:"page"             json:"page,omitempty"`
	Grpc            []Grpc           `cfg:"grpc"             json:"grpc,omitempty"`
	Swagger         []Swagger        `cfg:"swagger"          json:"swagger,omitempty"`
	SwaggerSettings *SwaggerSettings `cfg:"swagger_settings" json:"swagger_settings,omitempty"`
}

type Group struct {
	Name     string    `cfg:"name"     json:"name"`
	Services []Service `cfg:"services" json:"services,omitempty"`
	Groups   []Group   `cfg:"groups"   json:"groups,omitempty"`
}

type Home struct {
	// Type can be HTML or MARKDOWN
	Type    string `cfg:"type"    json:"type,omitempty"`
	Content string `cfg:"content" json:"content,omitempty"`
}

type Iframe struct {
	Name string `cfg:"name" json:"name,omitempty"`
	Path string `cfg:"path" json:"path,omitempty"`
	URL  string `cfg:"url"  json:"url,omitempty"`
}

// Page for iframe view.
type Page struct {
	Name      string `cfg:"name"       json:"name,omitempty"`
	Path      string `cfg:"path"       json:"path,omitempty"`
	PathExtra string `cfg:"path_extra" json:"path_extra,omitempty"`

	URL    string       `cfg:"url"    json:"-"`
	Header HeaderHolder `cfg:"header" json:"-"`
	Host   bool         `cfg:"host"   json:"-"`
}

type HeaderHolder struct {
	Request  Header `cfg:"request"`
	Response Header `cfg:"response"`
}

type Header struct {
	SetHeader    map[string]string `cfg:"set_header"`
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
	Link                   string   `cfg:"link"                     json:"link"`
	Schemes                []string `cfg:"schemes"                  json:"schemes,omitempty"`
	Host                   string   `cfg:"host"                     json:"host,omitempty"`
	BasePath               string   `cfg:"base_path"                json:"base_path,omitempty"`
	BasePathPrefix         string   `cfg:"base_path_prefix"         json:"base_path_prefix,omitempty"`
	DisableAuthorizeButton *bool    `cfg:"disable_authorize_button" json:"disable_authorize_button,omitempty"`
}

func (m *View) GetInfo(ctx context.Context) (*Info, error) {
	if m.InfoURL == "" {
		return &m.Info, nil
	}

	if info := m.infoCache(); info != nil {
		return info, nil
	}

	info, err := m.infoRequest(ctx, true)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (m *View) infoCache() *Info {
	m.infoURLMutex.RLock()
	defer m.infoURLMutex.RUnlock()

	if m.infoURLTime.After(time.Now().Add(-DefaultInfoDelay)) {
		return m.infoTmp
	}

	return nil
}

func (m *View) infoRequest(ctx context.Context, cached bool) (*Info, error) {
	m.infoURLMutex.Lock()
	defer m.infoURLMutex.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.InfoURL, nil)
	if err != nil {
		if cached && m.infoTmp != nil {
			slog.Error("failed to create request", "error", err.Error())

			return m.infoTmp, nil
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
		if cached && m.infoTmp != nil {
			slog.Error("failed to do request", "error", err.Error())

			return m.infoTmp, nil
		}

		return nil, err
	}

	var info Info
	if err := m.decoder.Decode(strings.ToLower(m.InfoURLType), bytes.NewReader(body), &info); err != nil {
		return nil, err
	}

	m.infoTmp = &info
	m.infoURLTime = time.Now()

	return &info, nil
}

func (m *View) InformationUI(w http.ResponseWriter, r *http.Request) {
	info, err := m.GetInfo(r.Context())
	if err != nil {
		httputil.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})
		return
	}

	httputil.JSON(w, http.StatusOK, info)

	return
}
