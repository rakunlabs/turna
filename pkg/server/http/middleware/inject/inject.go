package inject

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/dustin/go-humanize"
	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/model"
)

type Inject struct {
	PathMap map[string][]InjectContent `cfg:"path_map"`

	// ContentTypes limits rewriting to responses with these media types,
	// e.g. text/html; a trailing /* matches a whole type. Default all.
	ContentTypes []string `cfg:"content_types"`
	// Statuses limits rewriting to responses with these statuses, default all.
	Statuses []int `cfg:"statuses"`
	// MaxBodySize is the largest body rewritten, larger responses pass
	// unchanged. Default 10MB.
	MaxBodySize string `cfg:"max_body_size"`

	maxBody int64
}

type InjectContent struct {
	reg *regexp.Regexp
	// Regex is the regex to match the content.
	//  - If regex is not empty, Old will be ignored.
	Regex string `cfg:"regex"`
	// Old is the old content to replace.
	Old        string `cfg:"old"`
	old        []byte
	New        string `cfg:"new"`
	new        []byte
	AddPrefix  string `cfg:"add_prefix"`
	AddPostfix string `cfg:"add_postfix"`

	// Value from load name, key value and type is map[string]any
	Value      string `cfg:"value"`
	valueBytes []oldNew

	Delay time.Duration `cfg:"delay"`
}

type oldNew struct {
	Old []byte `cfg:"old"`
	New []byte `cfg:"new"`
}

func (s *Inject) values(loadName string) ([]oldNew, error) {
	v, ok := render.Data[loadName].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("inject value %s is not map[string]any", loadName)
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

func (s *Inject) Middleware() (func(http.Handler) http.Handler, error) {
	maxBody := s.MaxBodySize
	if maxBody == "" {
		maxBody = "10MB"
	}

	size, err := humanize.ParseBytes(maxBody)
	if err != nil || size == 0 || size > 1<<40 {
		return nil, fmt.Errorf("inject max_body_size %q is invalid", s.MaxBodySize)
	}
	s.maxBody = int64(size)

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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// check if need inject
			if s.isNotNeed(r) {
				next.ServeHTTP(w, r)

				return
			}

			rec := &customResponseRecorder{
				ResponseWriter: w,
				body:           new(bytes.Buffer),
				rewrite:        s.rewritable,
				limit:          s.maxBody,
			}

			next.ServeHTTP(rec, r)
			rec.decide(http.StatusOK)

			if !rec.buffering {
				return
			}

			bodyBytes := rec.body.Bytes()

			encoding := rec.Header().Get("Content-Encoding")
			if encoding == "gzip" {
				gzipReader, err := gzip.NewReader(bytes.NewReader(bodyBytes))
				if err != nil {
					httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

					return
				}

				bodyBytes, err = io.ReadAll(gzipReader)
				_ = gzipReader.Close()
				if err != nil {
					httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

					return
				}
			}

			urlPath := r.URL.Path
			for pathValue := range s.PathMap {
				if ok, _ := doublestar.Match(pathValue, urlPath); ok {
					for _, injectContent := range s.PathMap[pathValue] {
						if injectContent.Delay > 0 {
							time.Sleep(injectContent.Delay)
						}

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

						if len(injectContent.old) > 0 {
							bodyBytes = bytes.ReplaceAll(bodyBytes, injectContent.old, injectContent.new)
						}

						if injectContent.AddPrefix != "" {
							bodyBytes = append([]byte(injectContent.AddPrefix), bodyBytes...)
						}

						if injectContent.AddPostfix != "" {
							bodyBytes = append(bodyBytes, []byte(injectContent.AddPostfix)...)
						}
					}
				}
			}

			if encoding == "gzip" {
				var buf bytes.Buffer
				gzipWriter := gzip.NewWriter(&buf)
				_, err := gzipWriter.Write(bodyBytes)
				if err == nil {
					err = gzipWriter.Close()
				}
				if err != nil {
					httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

					return
				}

				bodyBytes = buf.Bytes()
			}

			rec.ResponseWriter.Header().Set(httputil.HeaderContentLength, strconv.Itoa(len(bodyBytes)))
			rec.ResponseWriter.WriteHeader(rec.status)

			_, _ = rec.ResponseWriter.Write(bodyBytes)
		})
	}, nil
}

func (s *Inject) isNotNeed(r *http.Request) bool {
	if len(s.PathMap) == 0 {
		return true
	}

	// WebSocket upgrades need the raw ResponseWriter so the proxy can
	// hijack the connection; buffering would break the upgrade.
	if httputil.IsWebSocket(r) {
		return true
	}

	isNeed := false

	if s.PathMap != nil {
		urlPath := r.URL.Path
		for pathValue := range s.PathMap {
			if ok, _ := doublestar.Match(pathValue, urlPath); ok {
				isNeed = true
				break
			}
		}
	}

	return !isNeed
}

// rewritable reports whether a response is buffered to be rewritten.
func (s *Inject) rewritable(status int, h http.Header) bool {
	if len(s.Statuses) > 0 && !slices.Contains(s.Statuses, status) {
		return false
	}

	// only gzip can be decoded
	if enc := h.Get("Content-Encoding"); enc != "" && enc != "gzip" {
		return false
	}

	if cl, err := strconv.ParseInt(h.Get(httputil.HeaderContentLength), 10, 64); err == nil && cl > s.maxBody {
		return false
	}

	if len(s.ContentTypes) > 0 {
		mediaType, _, _ := mime.ParseMediaType(h.Get("Content-Type"))

		matched := false
		for _, ct := range s.ContentTypes {
			ct = strings.ToLower(strings.TrimSpace(ct))
			if ct == mediaType || (strings.HasSuffix(ct, "/*") && strings.HasPrefix(mediaType, strings.TrimSuffix(ct, "*"))) {
				matched = true

				break
			}
		}

		if !matched {
			return false
		}
	}

	return !strings.HasPrefix(h.Get("Content-Type"), "text/event-stream")
}

// customResponseRecorder buffers a response when rewrite accepts it, else
// passes it through. A buffered body over limit is flushed and the rest
// streamed unchanged.
type customResponseRecorder struct {
	http.ResponseWriter
	body *bytes.Buffer

	rewrite func(status int, h http.Header) bool
	limit   int64

	status    int
	decided   bool
	buffering bool
}

func (r *customResponseRecorder) decide(code int) {
	if r.decided {
		return
	}

	r.decided = true
	r.status = code
	r.buffering = r.rewrite(code, r.Header())

	if !r.buffering {
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *customResponseRecorder) Write(b []byte) (int, error) {
	r.decide(http.StatusOK)

	if !r.buffering {
		return r.ResponseWriter.Write(b)
	}

	if int64(r.body.Len()+len(b)) > r.limit {
		r.buffering = false
		r.ResponseWriter.WriteHeader(r.status)

		if _, err := r.ResponseWriter.Write(r.body.Bytes()); err != nil {
			return 0, err
		}

		r.body.Reset()

		return r.ResponseWriter.Write(b)
	}

	return r.body.Write(b)
}

func (r *customResponseRecorder) WriteHeader(code int) {
	// 1xx responses are followed by the final one, 101 is final
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		r.ResponseWriter.WriteHeader(code)

		return
	}

	r.decide(code)
}

func (r *customResponseRecorder) Flush() {
	if r.decided && !r.buffering {
		_ = http.NewResponseController(r.ResponseWriter).Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer,
// e.g. to hijack the connection for protocol upgrades.
func (r *customResponseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

var _ http.Flusher = (*customResponseRecorder)(nil)
