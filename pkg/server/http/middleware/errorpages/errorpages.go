// Package errorpages replaces error responses with custom pages.
package errorpages

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	texttemplate "text/template"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/replay"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/statuscode"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

type Errors struct {
	// Statuses that are replaced, e.g. 404 or 500-599.
	Statuses []string `cfg:"statuses"`

	// Pages are page bodies by status or range, e.g. "404" or "500-599".
	// They are Go templates with .Status, .StatusText, .Method, .Path, .Host
	// and .RequestID.
	Pages map[string]string `cfg:"pages"`
	// Dir holds pages named by status, e.g. 404.html, 5xx.html and
	// error.html, used when Pages has no match.
	Dir string `cfg:"dir"`
	// ContentType of Pages and Dir pages, default text/html; charset=utf-8.
	ContentType string `cfg:"content_type"`

	// Service is a middleware chain answering error pages, e.g. a service
	// to an error page server. Query is the path sent to it, default
	// /{status}.
	Service []string `cfg:"service"`
	Query   string   `cfg:"query"`
}

// Refs returns the names of the middlewares the service uses.
func (m *Errors) Refs() []string {
	return m.Service
}

// maxServiceBody is the largest page read from Service.
const maxServiceBody = 1 << 20

// executor is a html/template or text/template template.
type executor interface {
	Execute(w io.Writer, data any) error
}

// parser parses pages with html/template for HTML content types, which
// escapes the request values in them, and text/template otherwise.
type parser func(name, text string) (executor, error)

func newParser(contentType string) parser {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return func(name, text string) (executor, error) {
			return htmltemplate.New(name).Parse(text)
		}
	}

	return func(name, text string) (executor, error) {
		return texttemplate.New(name).Parse(text)
	}
}

type page struct {
	codes statuscode.Set
	tpl   executor
}

type pageData struct {
	Status     int
	StatusText string
	Method     string
	Path       string
	Host       string
	RequestID  string
}

type handler struct {
	statuses    statuscode.Set
	pages       []page
	dir         map[string]executor
	contentType string
	service     http.Handler
	query       string
}

func (m *Errors) Middleware() (func(http.Handler) http.Handler, error) {
	if len(m.Statuses) == 0 {
		return nil, errors.New("errors statuses are required, e.g. [404, 500-599]")
	}

	h := &handler{contentType: m.ContentType, query: m.Query}

	var err error
	if h.statuses, err = statuscode.Parse("errors", m.Statuses); err != nil {
		return nil, err
	}

	// lower statuses have no body or are not errors
	if h.statuses.Min() < 400 {
		return nil, fmt.Errorf("errors statuses must be 400 or above, got %d", h.statuses.Min())
	}

	if h.contentType == "" {
		h.contentType = httputil.MIMETextHTMLCharsetUTF8
	}

	parse := newParser(h.contentType)

	if h.query == "" {
		h.query = "/{status}"
	}

	sources := 0

	for k, v := range m.Pages {
		codes, err := statuscode.Parse("errors pages", []string{k})
		if err != nil {
			return nil, err
		}

		tpl, err := parse(k, v)
		if err != nil {
			return nil, fmt.Errorf("errors page %s: %w", k, err)
		}

		h.pages = append(h.pages, page{codes: codes, tpl: tpl})
		sources++
	}

	// exact codes win over ranges, e.g. 404 over 400-499
	slices.SortStableFunc(h.pages, func(a, b page) int {
		if d := a.codes.Width() - b.codes.Width(); d != 0 {
			return d
		}

		return a.codes.Min() - b.codes.Min()
	})

	if m.Dir != "" {
		if h.dir, err = loadDir(m.Dir, parse); err != nil {
			return nil, err
		}

		sources++
	}

	if len(m.Service) > 0 {
		var chain []func(http.Handler) http.Handler
		for _, name := range m.Service {
			mw, err := registry.GlobalReg.GetHttpMiddleware(name)
			if err != nil {
				return nil, fmt.Errorf("errors service: %w", err)
			}

			chain = append(chain, mw...)
		}

		svc := http.NotFoundHandler()
		for i := len(chain) - 1; i >= 0; i-- {
			svc = chain[i](svc)
		}

		h.service = svc
		sources++
	}

	if sources == 0 {
		return nil, errors.New("errors needs pages, dir or service")
	}

	return h.middleware, nil
}

// loadDir reads pages once, so they can be served without file access on
// each error.
func loadDir(dir string, parse parser) (map[string]executor, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("errors dir: %w", err)
	}

	out := make(map[string]executor)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))

		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("errors dir: %w", err)
		}

		tpl, err := parse(e.Name(), string(b))
		if err != nil {
			return nil, fmt.Errorf("errors dir %s: %w", e.Name(), err)
		}

		key := strings.ToLower(name)
		if _, ok := out[key]; ok {
			return nil, fmt.Errorf("errors dir has more than one page for %s", key)
		}

		out[key] = tpl
	}

	return out, nil
}

func (h *handler) template(code int) executor {
	for _, p := range h.pages {
		if p.codes.Contains(code) {
			return p.tpl
		}
	}

	c := strconv.Itoa(code)
	for _, name := range []string{c, c[:1] + "xx", "error"} {
		if tpl, ok := h.dir[name]; ok {
			return tpl
		}
	}

	return nil
}

func (h *handler) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httputil.IsWebSocket(r) || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)

			return
		}

		ew := &errorWriter{ResponseWriter: w, intercept: h.statuses.Contains}
		next.ServeHTTP(ew, r)

		if ew.code == 0 {
			return
		}

		h.serve(w, r, ew.code)
	})
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request, code int) {
	if tpl := h.template(code); tpl != nil {
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, pageData{
			Status:     code,
			StatusText: http.StatusText(code),
			Method:     r.Method,
			Path:       r.URL.Path,
			Host:       r.Host,
			RequestID:  r.Header.Get("X-Request-Id"),
		}); err != nil {
			slog.Error("errors page template failed", "status", code, "error", err)
			httputil.HandleError(w, httputil.NewError("", nil, code))

			return
		}

		w.Header().Set(httputil.HeaderContentType, h.contentType)
		w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		w.WriteHeader(code)
		_, _ = w.Write(buf.Bytes())

		return
	}

	if h.service == nil {
		httputil.HandleError(w, httputil.NewError("", nil, code))

		return
	}

	target := strings.ReplaceAll(h.query, "{status}", strconv.Itoa(code))

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("", nil, code))

		return
	}

	req.Host = r.Host
	req.RemoteAddr = r.RemoteAddr
	for _, k := range []string{"Accept", "Accept-Language", "User-Agent", "X-Request-Id"} {
		if v := r.Header.Get(k); v != "" {
			req.Header.Set(k, v)
		}
	}

	rec := httptest.NewRecorder()
	h.service.ServeHTTP(rec, req)

	if rec.Code < 200 || rec.Code > 299 || rec.Body.Len() == 0 || rec.Body.Len() > maxServiceBody {
		slog.Warn("errors service failed", "status", rec.Code, "query", target)
		httputil.HandleError(w, httputil.NewError("", nil, code))

		return
	}

	if ct := rec.Header().Get(httputil.HeaderContentType); ct != "" {
		w.Header().Set(httputil.HeaderContentType, ct)
	}

	w.Header().Set("Content-Length", strconv.Itoa(rec.Body.Len()))
	w.WriteHeader(code)
	_, _ = w.Write(rec.Body.Bytes())
}

// errorWriter drops responses with intercepted statuses and passes the
// others through unchanged.
type errorWriter struct {
	http.ResponseWriter

	intercept func(int) bool
	code      int
	written   bool
}

// errorHeaders are removed from intercepted responses; the page sets its own.
var errorHeaders = []string{
	"Content-Type", "Content-Length", "Content-Encoding", "Content-Range", "Content-Disposition",
	"Content-Location", "Content-Language", "Etag", "Last-Modified", "Accept-Ranges",
}

func (w *errorWriter) WriteHeader(code int) {
	if w.written {
		return
	}

	if replay.Informational(code) {
		w.ResponseWriter.WriteHeader(code)

		return
	}

	w.written = true

	if w.intercept(code) {
		w.code = code
		for _, k := range errorHeaders {
			w.ResponseWriter.Header().Del(k)
		}

		return
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *errorWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if w.code != 0 {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}

func (w *errorWriter) Flush() {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if w.code != 0 {
		return
	}

	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *errorWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.written = true
	}

	return conn, rw, err
}

func (w *errorWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
