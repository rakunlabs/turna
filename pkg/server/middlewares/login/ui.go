package login

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/middlewares"
)

//go:embed _ui/dist/*
var uiFS embed.FS

func (m *Login) SetView() (echo.MiddlewareFunc, error) {
	f, err := fs.Sub(uiFS, "_ui/dist")
	if err != nil {
		return nil, err
	}

	folder := middlewares.Folder{
		Index:          true,
		StripIndexName: true,
		SPA:            true,
		Browse:         false,
		PrefixPath:     m.Path.Base,
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

func (m *Login) View(c echo.Context) error {
	return m.UI.embedUIFunc(c)
}
