package login

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/worldline-go/turna/pkg/server/http/middleware/folder"
)

//go:embed _ui/dist/*
var uiFS embed.FS

func (m *Login) SetView() (func(http.Handler) http.Handler, error) {
	f, err := fs.Sub(uiFS, "_ui/dist")
	if err != nil {
		return nil, err
	}

	folder := folder.Folder{
		Index:          true,
		StripIndexName: true,
		SPA:            true,
		Browse:         false,
		PrefixPath:     m.Path.Base,
		CacheRegex: []*folder.RegexCacheStore{
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

func (m *Login) View(w http.ResponseWriter, r *http.Request) error {
	m.UI.embedUI.ServeHTTP(w, r)

	return nil
}
