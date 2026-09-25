// Package compress compresses HTTP responses with zstd, brotli or gzip
// according to the client's Accept-Encoding header.
package compress

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

const (
	encGzip   = "gzip"
	encBrotli = "br"
	encZstd   = "zstd"
)

var defaultExcludedContentTypes = []string{
	"image/*",
	"video/*",
	"audio/*",
	"font/woff",
	"font/woff2",
	"application/zip",
	"application/gzip",
	"application/x-gzip",
	"application/zstd",
	"application/x-7z-compressed",
	"application/x-rar-compressed",
	"application/octet-stream",
	"application/grpc",
}

// uncompressedImages are image types that still benefit from compression.
var uncompressedImages = []string{"image/svg+xml", "image/bmp", "image/x-icon", "image/vnd.microsoft.icon"}

type Compress struct {
	// Encodings in server preference order, default is [zstd, br, gzip].
	// When the client accepts several with the same quality, the first one wins.
	Encodings []string `cfg:"encodings"`
	// MinLength is the minimum response size in bytes to compress, default is 1024.
	MinLength int `cfg:"min_length"`
	// ExcludedContentTypes are not compressed, supports "type/*" wildcards.
	// Default list covers already compressed formats (images, video, archives).
	ExcludedContentTypes []string `cfg:"excluded_content_types"`
	// IncludedContentTypes, when set, only these content types are compressed.
	IncludedContentTypes []string `cfg:"included_content_types"`

	GzipLevel   *int `cfg:"gzip_level"`
	BrotliLevel *int `cfg:"brotli_level"`
	ZstdLevel   *int `cfg:"zstd_level"`
}

func (m *Compress) Middleware() (func(http.Handler) http.Handler, error) {
	encodings := m.Encodings
	if len(encodings) == 0 {
		encodings = []string{encZstd, encBrotli, encGzip}
	}

	pools := make(map[string]*encoderPool, len(encodings))
	order := make([]string, 0, len(encodings))

	for _, enc := range encodings {
		enc = strings.ToLower(strings.TrimSpace(enc))
		if _, ok := pools[enc]; ok {
			return nil, fmt.Errorf("duplicate encoding %q", enc)
		}

		var (
			p   *encoderPool
			err error
		)

		switch enc {
		case encGzip:
			p, err = newGzipPool(m.GzipLevel)
		case encBrotli:
			p, err = newBrotliPool(m.BrotliLevel)
		case encZstd:
			p, err = newZstdPool(m.ZstdLevel)
		default:
			return nil, fmt.Errorf("unsupported encoding %q (supported: %s, %s, %s)", enc, encZstd, encBrotli, encGzip)
		}

		if err != nil {
			return nil, err
		}

		pools[enc] = p
		order = append(order, enc)
	}

	minLength := m.MinLength
	if minLength <= 0 {
		minLength = 1024
	}

	excluded := m.ExcludedContentTypes
	if excluded == nil {
		excluded = defaultExcludedContentTypes
	}

	filter := contentTypeFilter{
		excluded: normalizeTypes(excluded),
		included: normalizeTypes(m.IncludedContentTypes),
		// default exclusions of image/* must not drop compressible images
		allowImages: m.ExcludedContentTypes == nil,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WebSocket upgrades need the raw ResponseWriter so the proxy can
			// hijack the connection.
			if httputil.IsWebSocket(r) {
				next.ServeHTTP(w, r)

				return
			}

			addVary(w.Header(), "Accept-Encoding")

			enc := negotiate(r.Header.Values("Accept-Encoding"), order)
			if enc == "" || r.Method == http.MethodHead || r.Header.Get("Range") != "" {
				next.ServeHTTP(w, r)

				return
			}

			// the upstream must not compress again, turna compresses for the client
			r.Header.Del("Accept-Encoding")

			cw := &responseWriter{
				ResponseWriter: w,
				encoding:       enc,
				pool:           pools[enc],
				minLength:      minLength,
				filter:         &filter,
			}
			defer cw.close()

			next.ServeHTTP(cw, r)
		})
	}, nil
}

// negotiate returns the best encoding in order accepted by the client.
func negotiate(headerValues []string, order []string) string {
	qualities := parseQualities(headerValues)
	if len(qualities) == 0 {
		return ""
	}

	best, bestQ := "", 0.0
	for _, enc := range order {
		q, ok := qualities[enc]
		if !ok {
			q = qualities["*"]
		}

		if q > bestQ {
			best, bestQ = enc, q
		}
	}

	return best
}

func parseQualities(headerValues []string) map[string]float64 {
	qualities := make(map[string]float64)

	for _, headerValue := range headerValues {
		for token := range strings.SplitSeq(headerValue, ",") {
			name, params, _ := strings.Cut(token, ";")
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}

			q := 1.0
			for param := range strings.SplitSeq(params, ";") {
				key, value, ok := strings.Cut(param, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
					continue
				}

				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || parsed < 0 || parsed > 1 {
					parsed = 0
				}

				q = parsed
			}

			if prev, ok := qualities[name]; !ok || q < prev {
				qualities[name] = q
			}
		}
	}

	return qualities
}

func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for token := range strings.SplitSeq(existing, ",") {
			token = strings.TrimSpace(token)
			if token == "*" || strings.EqualFold(token, value) {
				return
			}
		}
	}

	header.Add("Vary", value)
}

type contentTypeFilter struct {
	excluded    []string
	included    []string
	allowImages bool
}

func normalizeTypes(types []string) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			out = append(out, t)
		}
	}

	return out
}

func matchType(pattern, contentType string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		return strings.HasPrefix(contentType, prefix+"/")
	}

	return pattern == contentType
}

func (f *contentTypeFilter) allowed(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))

	if len(f.included) > 0 {
		return slices.ContainsFunc(f.included, func(p string) bool { return matchType(p, mediaType) })
	}

	if f.allowImages && slices.Contains(uncompressedImages, mediaType) {
		return true
	}

	return !slices.ContainsFunc(f.excluded, func(p string) bool { return matchType(p, mediaType) })
}
