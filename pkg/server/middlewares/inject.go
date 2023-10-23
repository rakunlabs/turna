package middlewares

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

type Inject struct {
	// ContentMap is the mime type to inject like "text/html"
	ContentMap map[string][]InjectContent `cfg:"content_map"`
}

type InjectContent struct {
	Old string `cfg:"old"`
	New string `cfg:"new"`
}

func (s *Inject) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rec := &customResponseRecorder{
				ResponseWriter: c.Response().Writer,
				body:           new(bytes.Buffer),
			}
			c.Response().Writer = rec

			if err := next(c); err != nil {
				return err
			}

			bodyBytes := rec.body.Bytes()
			contentType := c.Response().Header().Get(echo.HeaderContentType)
			contentTypeCheck := strings.Split(contentType, ";")[0]
			for _, injectContent := range s.ContentMap[contentTypeCheck] {
				bodyBytes = bytes.ReplaceAll(bodyBytes, []byte(injectContent.Old), []byte(injectContent.New))
			}

			c.Response().Committed = false
			c.Response().Header().Set(echo.HeaderContentLength, strconv.Itoa(len(bodyBytes)))
			if rec.status != 0 {
				rec.ResponseWriter.WriteHeader(rec.status)
			}

			rec.ResponseWriter.Write(bodyBytes)

			return nil
		}
	}
}

type customResponseRecorder struct {
	http.ResponseWriter
	body *bytes.Buffer

	status int
}

func (r *customResponseRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *customResponseRecorder) WriteHeader(code int) {
	r.status = code
}
