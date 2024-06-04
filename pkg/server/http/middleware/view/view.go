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

	"github.com/labstack/echo/v4"
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

func (m *View) SetView() (echo.MiddlewareFunc, error) {
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

	regexPathGrpc := regexp.MustCompile(`/grpc/([^/]+)/?.*`)

	return func(_ echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if viewInfo, _ := strconv.ParseBool(c.QueryParam("view_info")); viewInfo {
				return m.InformationUI(c)
			}

			if v := regexPathGrpc.FindStringSubmatch(c.Request().URL.Path); len(v) > 1 {
				return m.GrpcUI(c, v[1])
			}

			return embedUIFunc(c)
		}
	}, nil
}
