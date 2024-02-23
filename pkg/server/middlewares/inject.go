package middlewares

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/render"
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
	old []byte
	New string `cfg:"new"`
	new []byte

	// Value from load name, key value and type is map[string]interface{}
	Value      string `cfg:"value"`
	valueBytes []oldNew
}

type oldNew struct {
	Old []byte `cfg:"old"`
	New []byte `cfg:"new"`
}

func (s *Inject) values(loadName string) ([]oldNew, error) {
	v, ok := render.GlobalRender.Data[loadName].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("inject value %s is not map[string]interface{}", loadName)
	}

	valuesOldNew := make([]oldNew, 0, len(v))
	for k, v := range v {
		valuesOldNew = append(valuesOldNew, oldNew{
			Old: []byte(k),
			New: []byte(fmt.Sprintf("%v", v)),
		})
	}

	return valuesOldNew, nil
}

func (s *Inject) Middleware() ([]echo.MiddlewareFunc, error) {
	if s.PathMap != nil {
		for pathValue := range s.PathMap {
			for i, injectContent := range s.PathMap[pathValue] {
				if injectContent.Value != "" {
					valuesOldNew, err := s.values(injectContent.Value)
					if err != nil {
						return nil, err
					}

					s.PathMap[pathValue][i].valueBytes = valuesOldNew

					continue
				}

				if injectContent.Regex != "" {
					var err error
					s.PathMap[pathValue][i].reg, err = regexp.Compile(injectContent.Regex)
					if err != nil {
						return nil, err
					}
				}

				s.PathMap[pathValue][i].old = []byte(injectContent.Old)
				s.PathMap[pathValue][i].new = []byte(injectContent.New)
			}
		}
	}

	if s.ContentMap != nil {
		for contentType := range s.ContentMap {
			for i, injectContent := range s.ContentMap[contentType] {
				if injectContent.Value != "" {
					valuesOldNew, err := s.values(injectContent.Value)
					if err != nil {
						return nil, err
					}

					s.PathMap[contentType][i].valueBytes = valuesOldNew

					continue
				}

				if injectContent.Regex != "" {
					var err error
					s.ContentMap[contentType][i].reg, err = regexp.Compile(injectContent.Regex)
					if err != nil {
						return nil, err
					}
				}

				s.ContentMap[contentType][i].old = []byte(injectContent.Old)
				s.ContentMap[contentType][i].new = []byte(injectContent.New)
			}
		}
	}

	return []echo.MiddlewareFunc{func(next echo.HandlerFunc) echo.HandlerFunc {
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
				if injectContent.valueBytes != nil {
					for _, valueOldNew := range injectContent.valueBytes {
						bodyBytes = bytes.ReplaceAll(bodyBytes, valueOldNew.Old, valueOldNew.New)
					}

					continue
				}

				if injectContent.reg != nil {
					bodyBytes = injectContent.reg.ReplaceAll(bodyBytes, injectContent.new)

					continue
				}

				bodyBytes = bytes.ReplaceAll(bodyBytes, injectContent.old, injectContent.new)
			}

			urlPath := c.Request().URL.Path
			for pathValue := range s.PathMap {
				if ok, _ := filepath.Match(pathValue, urlPath); ok {
					for _, injectContent := range s.PathMap[pathValue] {
						if injectContent.valueBytes != nil {
							for _, valueOldNew := range injectContent.valueBytes {
								bodyBytes = bytes.ReplaceAll(bodyBytes, valueOldNew.Old, valueOldNew.New)
							}

							continue
						}

						if injectContent.reg != nil {
							bodyBytes = injectContent.reg.ReplaceAll(bodyBytes, injectContent.new)

							continue
						}

						bodyBytes = bytes.ReplaceAll(bodyBytes, injectContent.old, injectContent.new)
					}
				}
			}

			c.Response().Committed = false
			c.Response().Header().Set(echo.HeaderContentLength, strconv.Itoa(len(bodyBytes)))
			if rec.status != 0 {
				rec.ResponseWriter.WriteHeader(rec.status)
			}

			_, _ = rec.ResponseWriter.Write(bodyBytes)

			return nil
		}
	}}, nil
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

func (r *customResponseRecorder) Flush() {
	// no-op
}

var _ http.Flusher = (*customResponseRecorder)(nil)
