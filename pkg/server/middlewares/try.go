package middlewares

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
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

func (m *Try) Middleware() (echo.MiddlewareFunc, error) {
	statusCodes := strings.Fields(strings.ReplaceAll(m.StatusCodes, ",", " "))
	rgx, err := regexp.Compile(m.Regex)
	if err != nil {
		return nil, fmt.Errorf("regexPath invalid regex: %s", err)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rec := &customResponseRecorder2{
				ResponseWriter: c.Response().Writer,
				body:           new(bytes.Buffer),
				header:         make(http.Header),
			}
			c.Response().Writer = rec

			if err := next(c); err != nil {
				return err
			}

			oldPath := c.Request().URL.Path
			newPath := rgx.ReplaceAllString(oldPath, m.Replacement)

			if oldPath == newPath || !IsInStatusCode(c.Response().Status, statusCodes) {
				if rec.status != 0 {
					rec.ResponseWriter.WriteHeader(rec.status)
				}

				_, err := rec.ResponseWriter.Write(rec.body.Bytes())
				return err
			}

			// cleanup response
			rec.body = new(bytes.Buffer)
			c.Response().Committed = false
			c.Request().URL.Path = newPath

			c.Logger().Debug("try middleware got "+strconv.Itoa(c.Response().Status)+" on "+oldPath+" going ", c.Request().URL.Path)

			rec.header = make(http.Header)
			if err := next(c); err != nil {
				return err
			}

			if rec.status != 0 {
				rec.ResponseWriter.WriteHeader(rec.status)
			}

			_, err := rec.ResponseWriter.Write(rec.body.Bytes())

			return err
		}
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
