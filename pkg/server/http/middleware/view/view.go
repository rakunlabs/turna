package view

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"path"
	"regexp"
	"sync"
	"time"

	"github.com/worldline-go/cache"
	"github.com/worldline-go/cache/store/memory"

	"github.com/worldline-go/klient"
	"github.com/worldline-go/turna/pkg/loader/decoder"
	"github.com/worldline-go/turna/pkg/server/http/middleware/folder"
	"github.com/worldline-go/turna/pkg/server/http/middleware/grpcui"
)

var DefaultInfoDelay = 4 * time.Second

type View struct {
	PrefixPath         string `cfg:"prefix_path"`
	Info               Info   `cfg:"info"`
	InfoURL            string `cfg:"info_url"`
	InfoURLType        string `cfg:"info_url_type"`
	InsecureSkipVerify bool   `cfg:"insecure_skip_verify"`

	infoURLMutex sync.RWMutex
	infoURLTime  time.Time
	infoTmp      *Info

	client *klient.Client `cfg:"-"`
	grpcUI GrpcUI         `cfg:"-"`
	pageUI PageUI         `cfg:"-"`

	decoder *decoder.Decoder `cfg:"-"`
}

type cacheKey struct {
	Name string
	Addr string
}

//go:embed _ui/dist/*
var uiFS embed.FS

func (m *View) SetView() (func(http.Handler) http.Handler, error) {
	f, err := fs.Sub(uiFS, "_ui/dist")
	if err != nil {
		return nil, err
	}

	folderM := folder.Folder{
		Index:          true,
		StripIndexName: true,
		SPA:            true,
		Browse:         false,
		PrefixPath:     m.PrefixPath,
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

	folderM.SetFs(http.FS(f))

	return folderM.Middleware()
}

func (m *View) Middleware(ctx context.Context, _ string) (func(http.Handler) http.Handler, error) {
	m.decoder = decoder.NewDecoder()

	if m.InfoURLType == "" {
		m.InfoURLType = "yaml"
	}

	// set caches
	cacheGrpcUI, err := cache.New[cacheKey, *grpcui.GrpcUI](ctx,
		memory.Store,
		cache.WithMaxItems(200),
		cache.WithTTL(30*time.Minute),
	)

	m.grpcUI.grpcUIMiddlewares = cacheGrpcUI

	cachePage, err := cache.New[cacheKey, *httputil.ReverseProxy](ctx,
		memory.Store,
		cache.WithMaxItems(200),
		cache.WithTTL(30*time.Minute),
	)

	m.pageUI.Handlers = cachePage

	setView, err := m.SetView()
	if err != nil {
		return nil, err
	}

	embedUI := setView(nil)

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

	regexPathGrpc := regexp.MustCompile(`/grpc/(.*)`)
	regexPathPage := regexp.MustCompile(`/page/(.*)`)

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == path.Join("/", m.PrefixPath, "/ui-info") {
				m.InformationUI(w, r)

				return
			}

			if v := regexPathGrpc.FindStringSubmatch(r.URL.Path); len(v) > 1 {
				m.GrpcUI(w, r, v[1])

				return
			}

			if v := regexPathPage.FindStringSubmatch(r.URL.Path); len(v) > 1 {
				m.Page(w, r, v[1])

				return
			}

			embedUI.ServeHTTP(w, r)
		})
	}, nil
}

func getIndex(v []string, i int) string {
	if len(v) > i {
		return v[i]
	}

	return ""
}
