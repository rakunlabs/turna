package login

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/folder"
)

//go:embed _ui/dist/*
var uiFS embed.FS

func (m *Login) SetUI() (func(http.Handler) http.Handler, error) {
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
				// stable, unhashed names; must revalidate across turna upgrades
				Regex:        `sdk\.(js|d\.ts)$`,
				CacheControl: "no-cache",
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

func (m *Login) UIHandler(w http.ResponseWriter, r *http.Request) {
	m.UI.uiHandler.ServeHTTP(w, r)
}

// sdkMethodsMarker is the placeholder inside the built sdk.js that carries a
// customized `path.methods` route to the SDK; see methodsURL in the SDK
// source. Untouched, the SDK falls back to the default {base}/auth/methods.
const sdkMethodsMarker = "__TURNA_METHODS_PATH__"

// SetSDK loads the login SDK module and its type declarations from the
// embedded UI build. Call it after the fixed paths are computed: when
// `path.methods` is overridden, the configured route is injected into the
// served module so SDK consumers keep working with custom paths.
func (m *Login) SetSDK() error {
	data, err := uiFS.ReadFile("_ui/dist/sdk.js")
	if err != nil {
		return fmt.Errorf("login sdk is not embedded: %w", err)
	}

	if m.Path.Methods != "" {
		data = bytes.ReplaceAll(data, []byte(sdkMethodsMarker), []byte(m.pathFixed.Methods))
	}

	m.sdkContent = data

	types, err := uiFS.ReadFile("_ui/dist/sdk.d.ts")
	if err != nil {
		return fmt.Errorf("login sdk types are not embedded: %w", err)
	}

	m.sdkTypesContent = types

	return nil
}

// SDKHandler serves the login SDK at the reserved {base}/auth/sdk.js route.
// It is dispatched before the session check and independently of
// ui.external_folder, so custom login pages can always load the embedded
// flows from a stable URL.
func (m *Login) SDKHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	// stable, unhashed name; must revalidate across turna upgrades
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(m.sdkContent)
}

// SDKTypesHandler serves the SDK TypeScript declarations at the reserved
// {base}/auth/sdk.d.ts route, for download into custom login UI projects.
func (m *Login) SDKTypesHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(m.sdkTypesContent)
}
