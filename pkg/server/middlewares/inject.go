package middlewares

import (
	"bytes"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

type Inject struct {
	PathMap map[string][]InjectContent `cfg:"path_map"`
	// ContentMap is the mime type to inject like "text/html"
	ContentMap map[string][]InjectContent `cfg:"content_map"`
}

type InjectContent struct {
	reg *regexp.Regexp
	// Regex is the regex to match the content.
	//  - If regex is not empty, Old will be ignored.
	Regex string `cfg:"regex"`
	// Old is the old content to replace.
	Old string `cfg:"old"`
	New string `cfg:"new"`
}

func (s *Inject) Middleware() echo.MiddlewareFunc {
	if s.PathMap != nil {
		for pathValue := range s.PathMap {
			for i, injectContent := range s.PathMap[pathValue] {
				if injectContent.Regex != "" {
					s.PathMap[pathValue][i].reg = regexp.MustCompile(injectContent.Regex)
				}
			}
		}
	}

	if s.ContentMap != nil {
		for contentType := range s.ContentMap {
			for i, injectContent := range s.ContentMap[contentType] {
				if injectContent.Regex != "" {
					s.ContentMap[contentType][i].reg = regexp.MustCompile(injectContent.Regex)
				}
			}
		}
	}

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
				if injectContent.reg != nil {
					bodyBytes = injectContent.reg.ReplaceAll(bodyBytes, []byte(injectContent.New))

					continue
				}

				bodyBytes = bytes.ReplaceAll(bodyBytes, []byte(injectContent.Old), []byte(injectContent.New))
			}

			urlPath := c.Request().URL.Path
			for pathValue := range s.PathMap {
				if ok, _ := filepath.Match(pathValue, urlPath); ok {
					for _, injectContent := range s.PathMap[pathValue] {
						if injectContent.reg != nil {
							bodyBytes = injectContent.reg.ReplaceAll(bodyBytes, []byte(injectContent.New))

							continue
						}

						bodyBytes = bytes.ReplaceAll(bodyBytes, []byte(injectContent.Old), []byte(injectContent.New))
					}
				}
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
