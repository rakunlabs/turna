package auth

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"

	adaswagger "github.com/rakunlabs/ada/handler/swagger"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/folder"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

var (
	//go:embed _ui/dist/*
	uiFS embed.FS

	//go:embed files/*
	swaggerFS embed.FS
)

var swaggerDocCache struct {
	once sync.Once
	doc  []byte
	err  error
}

// SwaggerDocAPI serves the embedded OpenAPI document with basePath set to
// the configured prefix path.
func (m *Auth) SwaggerDocAPI(w http.ResponseWriter, _ *http.Request) {
	swaggerDocCache.once.Do(func() {
		raw, err := swaggerFS.ReadFile("files/swagger.json")
		if err != nil {
			swaggerDocCache.err = err
			return
		}

		doc := map[string]any{}
		if err := json.Unmarshal(raw, &doc); err != nil {
			swaggerDocCache.err = err
			return
		}

		doc["basePath"] = m.PrefixPath

		swaggerDocCache.doc, swaggerDocCache.err = json.Marshal(doc)
	})

	if swaggerDocCache.err != nil {
		httputil.HandleError(w, httputil.NewError("cannot load swagger document", swaggerDocCache.err, http.StatusInternalServerError))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(swaggerDocCache.doc)
}

// SwaggerUIHandler returns the swagger UI page configured to load the
// embedded OpenAPI document.
func (m *Auth) SwaggerUIHandler() http.HandlerFunc {
	return adaswagger.Handler(
		adaswagger.WithConfigFns(httpSwagger.URL("swagger.json")),
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
