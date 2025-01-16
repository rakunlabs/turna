package iam

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/worldline-go/turna/pkg/server/http/middleware/folder"
)

var (
	//go:embed files/*
	swaggerFS embed.FS

	//go:embed _ui/dist/*
	uiFS embed.FS
)

func (m *Iam) SwaggerMiddleware() (func(http.Handler) http.Handler, error) {
	f, err := fs.Sub(swaggerFS, "files")
	if err != nil {
		return nil, err
	}

	folderM := folder.Folder{
		Index:          false,
		StripIndexName: true,
		SPA:            false,
		Browse:         false,
		PrefixPath:     m.PrefixPath + "/swagger/",
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

func (m *Iam) UIMiddleware() (func(http.Handler) http.Handler, error) {
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
