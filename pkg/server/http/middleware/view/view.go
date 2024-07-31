package view

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/folder"
	"github.com/worldline-go/klient"
)

var DefaultInfoDelay = 4 * time.Second

type View struct {
	PrefixPath         string `cfg:"prefix_path"`
	Info               Info   `cfg:"info"`
	InfoURL            string `cfg:"info_url"`
	InfoURLType        string `cfg:"info_url_type"`
	InsecureSkipVerify bool   `cfg:"insecure_skip_verify"`

	infoURLMutex sync.Mutex
	infoURLTime  time.Time
	infoTmpBody  []byte

	client *klient.Client `cfg:"-"`
	grpcUI GrpcUI         `cfg:"-"`
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

func (m *View) Middleware(_ context.Context, _ string) (func(http.Handler) http.Handler, error) {
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

	regexPathGrpc := regexp.MustCompile(`/grpc/([^/]+)/?.*`)

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if viewInfo, _ := strconv.ParseBool(query.Get("view_info")); viewInfo {
				m.InformationUI(w, r)

				return
			}

			if v := regexPathGrpc.FindStringSubmatch(r.URL.Path); len(v) > 1 {
				_ = m.GrpcUI(w, r, v[1])

				return
			}

			embedUI.ServeHTTP(w, r)
		})
	}, nil
}
