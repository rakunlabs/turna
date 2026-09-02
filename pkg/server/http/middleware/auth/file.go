package auth

import (
	"embed"
	"io/fs"
	"net/http"

	adaswagger "github.com/rakunlabs/ada/handler/swagger"
	_ "github.com/rakunlabs/turna/pkg/server/http/middleware/auth/docs"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/folder"
)

var (
	//go:embed _ui/dist/*
	uiFS embed.FS
)

// SwaggerUIHandler serves the generated OpenAPI document and Swagger UI.
func (m *Auth) SwaggerUIHandler() http.HandlerFunc {
	return adaswagger.Handler(
		adaswagger.WithBasePath(m.PrefixPath),
	)
}

func (m *Auth) UIMiddleware() (func(http.Handler) http.Handler, error) {
	f, err := fs.Sub(uiFS, "_ui/dist")
	if err != nil {
		return nil, err
	}

	folderM := folder.Folder{
		Index:          true,
		StripIndexName: true,
		SPA:            true,
		Browse:         false,
		PrefixPath:     m.PrefixPath + "/ui/",
		CacheRegex: []*folder.RegexCacheStore{
			{
				Regex:        `.*`,
				CacheControl: "no-store",
			},
		},
	}

	folderM.SetFs(http.FS(f))

	return folderM.Middleware()
}
