package try

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type Try struct {
	Regex       string `cfg:"regex"`
	Replacement string `cfg:"replacement"`

	// StatusCodes is a comma separated list of status codes to redirect
	//   300-399, 401, 403, 404, 405, 408, 410, 500-505
	StatusCodes string `cfg:"status_codes"`
}

func IsInStatusCode(code int, list []string) bool {
	codeStr := strconv.Itoa(code)

	for _, c := range list {
		if c == codeStr {
			return true
		}

		if strings.Contains(c, "-") {
			codes := strings.Split(c, "-")
			if len(codes) != 2 {
				continue
			}

			start, err := strconv.Atoi(codes[0])
			if err != nil {
				continue
			}

			end, err := strconv.Atoi(codes[1])
			if err != nil {
				continue
			}

			if code >= start && code <= end {
				return true
			}
		}
	}

	return false
}

func (m *Try) Middleware() (func(http.Handler) http.Handler, error) {
	statusCodes := strings.Fields(strings.ReplaceAll(m.StatusCodes, ",", " "))
	rgx, err := regexp.Compile(m.Regex)
	if err != nil {
		return nil, fmt.Errorf("regexPath invalid regex: %s", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &customResponseRecorder2{
				ResponseWriter: w,
				body:           new(bytes.Buffer),
				header:         make(http.Header),
			}

			next.ServeHTTP(rec, r)

			oldPath := r.URL.Path
			newPath := rgx.ReplaceAllString(oldPath, m.Replacement)

			if oldPath == newPath || !IsInStatusCode(rec.status, statusCodes) {
				if rec.status != 0 {
					rec.ResponseWriter.WriteHeader(rec.status)
				}

				_, _ = rec.ResponseWriter.Write(rec.body.Bytes())

				return
			}

			// cleanup response
			rec.body = new(bytes.Buffer)
			r.URL.Path = newPath

			slog.Debug("try middleware", "status", rec.status, "old_path", oldPath, "new_path", r.URL.Path)

			rec.header = make(http.Header)
			next.ServeHTTP(rec, r)

			if rec.status != 0 {
				rec.ResponseWriter.WriteHeader(rec.status)
			}

			_, _ = rec.ResponseWriter.Write(rec.body.Bytes())
		})
	}, nil
}

type customResponseRecorder2 struct {
	http.ResponseWriter
	body *bytes.Buffer

	status int
	header http.Header
}

func (r *customResponseRecorder2) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *customResponseRecorder2) WriteHeader(code int) {
	r.status = code
}

func (r *customResponseRecorder2) Header() http.Header {
	return r.header
}

func (r *customResponseRecorder2) Flush() {
	// no-op
}

var _ http.Flusher = (*customResponseRecorder2)(nil)
