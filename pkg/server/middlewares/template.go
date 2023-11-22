package middlewares

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"github.com/rytsh/mugo/pkg/fstore"
	"github.com/rytsh/mugo/pkg/templatex"
	"github.com/worldline-go/logz"
	"github.com/worldline-go/turna/pkg/render"
)

type Template struct {
	Template string `cfg:"template"`
	// RawBody is the value of the body is raw or is an interface{}
	RawBody bool `cfg:"raw_body"`
	// Status code to return, default is from response
	StatusCode int `cfg:"status_code"`
	// Value from load name, key value and type is map[string]interface{}
	Value string `cfg:"value"`
	// Additional is additional values for the template
	Additional map[string]interface{} `cfg:"additional"`
	// Headers are additional to return
	Headers map[string]string `cfg:"headers"`
	// ApplyStatusCodes on specific status codes
	ApplyStatusCodes []int `cfg:"apply_status_codes"`

	// Trust is allow to use powerful functions
	Trust bool `cfg:"trust"`
	// WorkDir is the directory for some functions
	WorkDir string `cfg:"work_dir"`
	// Delims is the delimiters for the template
	Delims []string `cfg:"delims"`

	tpl   *templatex.Template
	value map[string]interface{}
}

func (s *Template) render(c echo.Context, body []byte) ([]byte, error) {
	var bodyPass interface{}
	if !s.RawBody {
		if err := json.Unmarshal(body, &bodyPass); err != nil {
			return nil, err
		}
	} else {
		bodyPass = body
	}

	// get all data for the template
	data := map[string]interface{}{
		"body":         bodyPass,
		"body_raw":     body,
		"method":       c.Request().Method,
		"headers":      c.Request().Header,
		"query_params": c.QueryParams(),
		"cookies":      c.Cookies(),
		"path":         c.Request().URL.Path,
		"value":        s.value,
		"additional":   s.Additional,
	}

	var buf bytes.Buffer
	if err := s.tpl.Execute(
		templatex.WithIO(&buf),
		templatex.WithContent(s.Template),
		templatex.WithData(data),
	); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (s *Template) Middleware() (echo.MiddlewareFunc, error) {
	if s.Delims != nil {
		if len(s.Delims) != 2 {
			return nil, fmt.Errorf("delims must be a pair of strings")
		}
	}

	s.tpl = templatex.New(templatex.WithAddFuncsTpl(
		fstore.FuncMapTpl(
			fstore.WithLog(logz.AdapterKV{Log: log.Logger}),
			fstore.WithTrust(s.Trust),
			fstore.WithWorkDir(s.WorkDir),
		),
	))

	if s.Value != "" {
		value, ok := render.GlobalRender.Data[s.Value].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("inject value %s is not map[string]interface{}", s.Value)
		}
		s.value = value
	}

	if s.Delims != nil {
		s.tpl.SetDelims(s.Delims[0], s.Delims[1])
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

			// do template job
			statusCodeRet := s.StatusCode

			doTemplate := false
			if len(s.ApplyStatusCodes) > 0 {
				for _, statusCode := range s.ApplyStatusCodes {
					if c.Response().Status == statusCode {
						doTemplate = true

						break
					}
				}
			} else {
				doTemplate = true
			}

			if doTemplate {
				var err error
				bodyBytes, err = s.render(c, bodyBytes)
				if err != nil {
					bodyBytes = []byte(err.Error())
					statusCodeRet = http.StatusInternalServerError
				}
			}

			// return part

			c.Response().Committed = false
			c.Response().Header().Set(echo.HeaderContentLength, strconv.Itoa(len(bodyBytes)))

			switch {
			case doTemplate:
				// add headers
				for k, v := range s.Headers {
					c.Response().Header().Set(k, v)
				}

				if statusCodeRet != 0 {
					rec.ResponseWriter.WriteHeader(statusCodeRet)
				}
			case rec.status != 0:
				rec.ResponseWriter.WriteHeader(rec.status)
			default:
				rec.ResponseWriter.WriteHeader(200)
			}

			_, _ = rec.ResponseWriter.Write(bodyBytes)

			return nil
		}
	}, nil
}
