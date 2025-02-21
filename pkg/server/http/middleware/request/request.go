package request

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/worldline-go/klient"
	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/model"
)

type Request struct {
	URLRgx  string            `cfg:"url_rgx"`
	URL     string            `cfg:"url"`
	Method  string            `cfg:"method"`
	Body    string            `cfg:"body"`
	Headers map[string]string `cfg:"headers"`

	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
	EnableRetry        bool `cfg:"enable_retry"`

	client *klient.Client
}

func (m *Request) Middleware() (func(http.Handler) http.Handler, error) {
	client, err := klient.New(
		klient.WithDisableBaseURLCheck(true),
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
		klient.WithDisableRetry(!m.EnableRetry),
		klient.WithDisableEnvValues(true),
		klient.WithLogger(slog.Default()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create klient client: %w", err)
	}

	m.client = client

	var rgx *regexp.Regexp
	if m.URLRgx != "" {
		rgx, err = regexp.Compile(m.URLRgx)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			url := m.URL
			if rgx != nil {
				url = rgx.ReplaceAllString(r.URL.Path, m.URL)
			}

			request, err := http.NewRequestWithContext(r.Context(), m.Method, url, strings.NewReader(m.Body))
			if err != nil {
				httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

				return
			}

			for k, v := range m.Headers {
				request.Header.Set(k, v)
			}

			var retStatus int
			var retBody []byte
			var retHeaders http.Header

			if err := m.client.Do(request, func(r *http.Response) error {
				retStatus = r.StatusCode
				retBody, err = io.ReadAll(r.Body)
				if err != nil {
					return err
				}

				retHeaders = r.Header

				return nil
			}); err != nil {
				httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

				return
			}

			header := w.Header()
			for k, v := range retHeaders {
				header[k] = v
			}

			w.WriteHeader(retStatus)
			w.Write(retBody)
		})
	}, nil
}
