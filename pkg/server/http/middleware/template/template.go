package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/rytsh/mugo/fstore"
	"github.com/rytsh/mugo/templatex"
	"github.com/worldline-go/logz"
	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
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

func (s *Template) render(r *http.Request, body []byte) ([]byte, error) {
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
		"method":       r.Method,
		"headers":      r.Header,
		"query_params": r.URL.Query(),
		"cookies":      r.Cookies(),
		"path":         r.URL.Path,
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

func (s *Template) Middleware() (func(http.Handler) http.Handler, error) {
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &customResponseRecorder{
				ResponseWriter: w,
				body:           new(bytes.Buffer),
			}

			next.ServeHTTP(rec, r)

			bodyBytes := rec.body.Bytes()

			// do template job
			statusCodeRet := s.StatusCode

			doTemplate := false
			if len(s.ApplyStatusCodes) > 0 {
				for _, statusCode := range s.ApplyStatusCodes {
					if rec.status == statusCode {
						doTemplate = true

						break
					}
				}
			} else {
				doTemplate = true
			}

			if doTemplate {
				var err error
				bodyBytes, err = s.render(r, bodyBytes)
				if err != nil {
					bodyBytes = []byte(err.Error())
					statusCodeRet = http.StatusInternalServerError
				}
			}

			// return part
			header := rec.ResponseWriter.Header()
			header.Set(httputil.HeaderContentLength, strconv.Itoa(len(bodyBytes)))

			switch {
			case doTemplate:
				// add headers
				for k, v := range s.Headers {
					header.Set(k, v)
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
		})
	}, nil
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
