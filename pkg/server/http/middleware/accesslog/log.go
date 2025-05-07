package accesslog

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

type AccessLog struct {
	Level   string `cfg:"level"`
	Message string `cfg:"message"`

	Path Path `cfg:"path"`

	LogDetails LogDetails `cfg:"log_details"`
}

const (
	LogTypeDebug = "debug"
	LogTypeInfo  = "info"
	LogTypeWarn  = "warn"
	LogTypeError = "error"
)

func Log(levelType, message string, args ...any) {
	switch levelType {
	case LogTypeDebug:
		slog.Debug(message, args...)
	case LogTypeInfo:
		slog.Info(message, args...)
	case LogTypeWarn:
		slog.Warn(message, args...)
	case LogTypeError:
		slog.Error(message, args...)
	default:
		slog.Info(message, args...)
	}
}

func (m *AccessLog) TypeCheck() error {
	if m.Level == "" {
		m.Level = LogTypeInfo
	}

	m.Level = strings.ToLower(m.Level)

	if slices.Contains([]string{LogTypeDebug, LogTypeInfo, LogTypeWarn, LogTypeError}, m.Level) {
		return nil
	}

	return fmt.Errorf("invalid log type: [%s]", m.Level)
}

func (m *AccessLog) Middleware() (func(http.Handler) http.Handler, error) {
	if err := m.TypeCheck(); err != nil {
		return nil, err
	}

	m.LogDetails.Default()

	if m.Message == "" {
		m.Message = "access log"
	}

	for i := range m.Path.Disabled {
		m.Path.Disabled[i].methods = NewMethod(m.Path.Disabled[i].Methods)
		if m.Path.Disabled[i].LogDetails == nil {
			m.Path.Disabled[i].LogDetails = &m.LogDetails
		} else {
			m.Path.Disabled[i].LogDetails.Default()
		}
	}

	for i := range m.Path.Enabled {
		m.Path.Enabled[i].methods = NewMethod(m.Path.Enabled[i].Methods)
		if m.Path.Enabled[i].LogDetails == nil {
			m.Path.Enabled[i].LogDetails = &m.LogDetails
		} else {
			m.Path.Enabled[i].LogDetails.Default()
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// check if the path is disabled
			for i := range m.Path.Disabled {
				if m.Path.Disabled[i].IsIn(r.URL.Path, r.Method) {
					next.ServeHTTP(w, r)

					return
				}
			}

			var logDetails *LogDetails
			for i := range m.Path.Enabled {
				if m.Path.Enabled[i].IsIn(r.URL.Path, r.Method) {
					logDetails = m.Path.Enabled[i].LogDetails
					break
				}
			}
			if logDetails == nil {
				next.ServeHTTP(w, r)

				return
			}

			start := time.Now()

			argsResponse := make([]any, 0, 20)
			argsRequest := make([]any, 0, 20)

			argsRequest = append(argsRequest,
				"method", r.Method,
				"path", r.URL.Path,
			)

			if requestID := r.Header.Get("X-Request-Id"); requestID != "" {
				argsRequest = append(argsRequest, "request_id", requestID)
			}

			if user := r.Header.Get("X-User"); user != "" {
				argsRequest = append(argsRequest, "user", user)
			}

			// get headers
			if logDetails.GetHeaders() {
				headers := make(map[string][]string, len(r.Header))
				for k, v := range r.Header {
					if slices.Contains(logDetails.SanitizeHeaders, k) {
						v = []string{"[REDACTED]"}
					}

					headers[k] = v
				}

				argsRequest = append(argsRequest, "headers", headers)
			}

			// read the body
			if logDetails.GetRequestBody() {
				// Read the body
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "Error reading request body", http.StatusInternalServerError)

					return
				}

				// Close the original body
				r.Body.Close()
				// Create a new body from our saved bytes
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				// Log the body
				switch {
				case len(bodyBytes) == 0:
					argsRequest = append(argsRequest, "body", nil)
				case logDetails.RequestBodySize == 0:
					argsRequest = append(argsRequest, "body", string(bodyBytes))
				case logDetails.RequestBodySize > 0 && len(bodyBytes) <= logDetails.RequestBodySize:
					argsRequest = append(argsRequest, "body", string(bodyBytes))
				case logDetails.RequestBodySize > 0 && len(bodyBytes) > logDetails.RequestBodySize:
					argsRequest = append(argsRequest, "body", string(bodyBytes[:logDetails.RequestBodySize]))
				}
			}

			rec := &customResponseRecorder{
				ResponseWriter: w,
				body:           new(bytes.Buffer),
			}

			next.ServeHTTP(rec, r)

			bodyBytes := rec.body.Bytes()

			argsResponse = append(argsResponse,
				"status", rec.status,
				"size", humanize.Bytes(uint64(len(bodyBytes))),
				"size_bytes", len(bodyBytes),
				"duration", time.Since(start).String(),
				"duration_ms", time.Since(start).Milliseconds(),
			)

			if logDetails.GetResponseBody() {
				switch {
				case len(bodyBytes) == 0:
					argsResponse = append(argsResponse, "body", nil)
				case logDetails.ResponseBodySize == 0:
					argsResponse = append(argsResponse, "body", string(bodyBytes))
				case logDetails.ResponseBodySize > 0 && len(bodyBytes) <= logDetails.ResponseBodySize:
					argsResponse = append(argsResponse, "body", string(bodyBytes))
				case logDetails.ResponseBodySize > 0 && len(bodyBytes) > logDetails.ResponseBodySize:
					argsResponse = append(argsResponse, "body", string(bodyBytes[:logDetails.ResponseBodySize]))
				}
			}

			Log(m.Level, m.Message, slog.Group("request", argsRequest...), slog.Group("response", argsResponse...))

			rec.WriteHeader(rec.status)
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
