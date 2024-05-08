package view

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middlewares"
	"github.com/worldline-go/klient"
)

type View struct {
	PrefixPath         string `cfg:"prefix_path"`
	Info               Info   `cfg:"info"`
	InfoURL            string `cfg:"info_url"`
	InfoURLType        string `cfg:"info_url_type"`
	InsecureSkipVerify bool   `cfg:"insecure_skip_verify"`

	client *klient.Client `cfg:"-"`
}

//go:embed _ui/dist/*
var uiFS embed.FS

func (m *View) SetView() (echo.MiddlewareFunc, error) {
	f, err := fs.Sub(uiFS, "_ui/dist")
	if err != nil {
		return nil, err
	}

	folder := middlewares.Folder{
		Index:          true,
		StripIndexName: true,
		SPA:            true,
		Browse:         false,
		PrefixPath:     m.PrefixPath,
		CacheRegex: []*middlewares.RegexCacheStore{
			{
				Regex:        `index\.html$`,
				CacheControl: "no-store",
			},
			{
				Regex:        `.*`,
				CacheControl: "public, max-age=259200",
			},
		},
	}

	folder.SetFs(http.FS(f))

	return folder.Middleware()
}

func (m *View) Middleware(_ context.Context, _ string) (echo.MiddlewareFunc, error) {
	setView, err := m.SetView()
	if err != nil {
		return nil, err
	}

	embedUIFunc := setView(nil)

	if m.InfoURL != "" {
		client, err := klient.New(
			klient.WithDisableBaseURLCheck(true),
			klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
			klient.WithDisableRetry(true),
			klient.WithDisableEnvValues(true),
		)
		if err != nil {
			return nil, err
		}

		m.client = client
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if viewInfo, _ := strconv.ParseBool(c.QueryParam("view_info")); viewInfo {
				return m.InformationUI(c)
			}

			return embedUIFunc(c)
		}
	}, nil
}
