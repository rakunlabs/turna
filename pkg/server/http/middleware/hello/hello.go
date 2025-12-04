package hello

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rytsh/mugo/fstore"
	"github.com/rytsh/mugo/templatex"
)

type Hello struct {
	// Message to return, default is "OK"
	Message string `cfg:"message"`
	// Status code to return, default is 200
	StatusCode int               `cfg:"status_code"`
	Headers    map[string]string `cfg:"headers"`

	ContentType string `cfg:"content_type"`

	// Template to render go template
	Template bool `cfg:"template"`
	// Trust is allow to use powerful functions
	Trust bool `cfg:"trust"`
	// WorkDir is the directory for some functions
	WorkDir string `cfg:"work_dir"`
	// Delims is the delimiters for the template
	Delims []string `cfg:"delims"`
}

func (h *Hello) Middleware() (func(http.Handler) http.Handler, error) {
	if h.Delims != nil {
		if len(h.Delims) != 2 {
			return nil, fmt.Errorf("delims must be a pair of strings")
		}
	}

	if h.StatusCode == 0 {
		h.StatusCode = http.StatusOK
	}

	if h.Message == "" {
		h.Message = "OK"
	}

	tpl := templatex.New(templatex.WithAddFuncMapWithOpts(func(o templatex.Option) map[string]any {
		return fstore.FuncMap(
			fstore.WithLog(slog.Default()),
			fstore.WithTrust(h.Trust),
			fstore.WithWorkDir(h.WorkDir),
		)
	}))

	if h.Delims != nil {
		tpl.SetDelims(h.Delims[0], h.Delims[1])
	}

	contentType := h.ContentType
	if contentType == "" {
		contentType = httputil.MIMETextPlainCharsetUTF8
	}

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range h.Headers {
				w.Header().Set(k, v)
			}

			message := h.Message

			if h.Template {
				// max body size is 4MB
				body, err := io.ReadAll(io.LimitReader(r.Body, 1<<22))
				if err != nil {
					httputil.HandleError(w, httputil.NewError("Cannot read body", err, http.StatusInternalServerError))
					return
				}

				r.Body.Close()

				// get all data for the template
				data := map[string]interface{}{
					"body":         body,
					"method":       r.Method,
					"headers":      r.Header,
					"query_params": r.URL.Query(),
					"cookies":      r.Cookies(),
					"path":         r.URL.Path,
					"host":         r.Host,
					"scheme":       r.URL.Scheme,
					"remote_addr":  r.RemoteAddr,
				}

				var buf bytes.Buffer
				if err = tpl.Execute(
					templatex.WithIO(&buf),
					templatex.WithContent(message),
					templatex.WithData(data),
				); err != nil {
					httputil.HandleError(w, httputil.NewError("Cannot execute template", err, http.StatusInternalServerError))
					return
				}

				message = buf.String()
			}

			httputil.Blob(w, h.StatusCode, contentType, []byte(message))
		})
	}, nil
}
