package request

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/rakunlabs/turna/pkg/server/model"
	"github.com/worldline-go/klient"
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

func (m *Request) Middleware() (echo.MiddlewareFunc, error) {
	client, err := klient.New(
		klient.WithDisableBaseURLCheck(true),
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
		klient.WithDisableRetry(!m.EnableRetry),
		klient.WithDisableEnvValues(true),
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

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			url := m.URL
			if rgx != nil {
				url = rgx.ReplaceAllString(c.Request().URL.Path, m.URL)
			}

			request, err := http.NewRequestWithContext(c.Request().Context(), m.Method, url, strings.NewReader(m.Body))
			if err != nil {
				return c.JSON(http.StatusInternalServerError, model.MetaData{Message: err.Error()})
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
				return c.JSON(http.StatusInternalServerError, model.MetaData{Message: err.Error()})
			}

			respose := c.Response()
			header := respose.Header()
			for k, v := range retHeaders {
				header[k] = v
			}

			respose.WriteHeader(retStatus)
			_, err = respose.Write(retBody)

			return err
		}
	}, nil
}
