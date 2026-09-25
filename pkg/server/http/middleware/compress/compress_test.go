package compress

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func decode(t *testing.T, enc string, body []byte) string {
	t.Helper()

	var r io.Reader
	switch enc {
	case encGzip:
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		r = gr
	case encBrotli:
		r = brotli.NewReader(bytes.NewReader(body))
	case encZstd:
		zr, err := zstd.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("zstd reader: %v", err)
		}
		defer zr.Close()
		r = zr
	default:
		return string(body)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("decode %s: %v", enc, err)
	}

	return string(out)
}

func newHandler(t *testing.T, m *Compress, next http.Handler) http.Handler {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	return mw(next)
}

var bigBody = strings.Repeat("hello turna ", 500)

func textHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})
}

func TestNegotiation(t *testing.T) {
	tests := []struct {
		accept string
		want   string
	}{
		{"gzip", encGzip},
		{"br", encBrotli},
		{"zstd", encZstd},
		{"gzip, deflate, br, zstd", encZstd},
		{"gzip, br", encBrotli},
		{"gzip;q=1, br;q=0.5", encGzip},
		{"*", encZstd},
		{"br;q=0, *", encZstd},
		{"zstd;q=0, br;q=0, gzip;q=0", ""},
		{"identity", ""},
		{"", ""},
	}

	h := newHandler(t, &Compress{}, textHandler(bigBody))

	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.accept != "" {
				req.Header.Set("Accept-Encoding", tt.accept)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("Content-Encoding"); got != tt.want {
				t.Fatalf("Content-Encoding = %q, want %q", got, tt.want)
			}

			if got := decode(t, tt.want, rec.Body.Bytes()); got != bigBody {
				t.Fatalf("body mismatch, len %d", len(got))
			}

			if rec.Header().Get("Vary") != "Accept-Encoding" {
				t.Fatalf("Vary = %q", rec.Header().Get("Vary"))
			}
		})
	}
}

func TestServerPreference(t *testing.T) {
	h := newHandler(t, &Compress{Encodings: []string{"gzip", "br"}}, textHandler(bigBody))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br, gzip, zstd")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != encGzip {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
}

func TestSkip(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
	}{
		{
			name:    "small body",
			handler: textHandler("small"),
		},
		{
			name: "excluded content type",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				_, _ = io.WriteString(w, bigBody)
			}),
		},
		{
			name: "already encoded",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				_, _ = io.WriteString(w, bigBody)
			}),
		},
		{
			name: "no-transform",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Cache-Control", "public, no-transform")
				_, _ = io.WriteString(w, bigBody)
			}),
		},
		{
			name: "not modified",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotModified)
			}),
		},
		{
			name:    "head",
			handler: textHandler(bigBody),
			method:  http.MethodHead,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandler(t, &Compress{}, tt.handler)

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}

			req := httptest.NewRequest(method, "/", nil)
			req.Header.Set("Accept-Encoding", "gzip, br, zstd")

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("Content-Encoding"); got == encBrotli || got == encZstd {
				t.Fatalf("unexpected Content-Encoding %q", got)
			}
		})
	}
}

func TestCompressibleImage(t *testing.T) {
	h := newHandler(t, &Compress{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = io.WriteString(w, bigBody)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != encBrotli {
		t.Fatalf("Content-Encoding = %q, want br", got)
	}
}

func TestIncludedContentTypes(t *testing.T) {
	h := newHandler(t, &Compress{IncludedContentTypes: []string{"application/json"}}, textHandler(bigBody))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
}

func TestContentLengthRemovedAndETagWeakened(t *testing.T) {
	h := newHandler(t, &Compress{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "6000")
		w.Header().Set("ETag", `"abc"`)
		_, _ = io.WriteString(w, bigBody)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length must be removed")
	}

	if got := rec.Header().Get("ETag"); got != `W/"abc"` {
		t.Fatalf("ETag = %q", got)
	}
}

func TestFlushStreams(t *testing.T) {
	for _, enc := range []string{encGzip, encBrotli, encZstd} {
		t.Run(enc, func(t *testing.T) {
			flushed := make(chan struct{})
			done := make(chan struct{})

			h := newHandler(t, &Compress{}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: one\n\n")
				w.(http.Flusher).Flush()
				close(flushed)
				<-done
			}))

			srv := httptest.NewServer(h)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			req.Header.Set("Accept-Encoding", enc)

			resp, err := (&http.Transport{DisableCompression: true}).RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			<-flushed

			if got := resp.Header.Get("Content-Encoding"); got != enc {
				t.Fatalf("Content-Encoding = %q", got)
			}

			var r io.Reader
			switch enc {
			case encGzip:
				r, err = gzip.NewReader(resp.Body)
			case encBrotli:
				r = brotli.NewReader(resp.Body)
			case encZstd:
				var zr *zstd.Decoder
				zr, err = zstd.NewReader(resp.Body)
				r = zr
			}
			if err != nil {
				t.Fatal(err)
			}

			buf := make([]byte, len("data: one\n\n"))
			if _, err := io.ReadFull(r, buf); err != nil {
				t.Fatalf("read flushed data: %v", err)
			}

			if string(buf) != "data: one\n\n" {
				t.Fatalf("got %q", buf)
			}

			close(done)
		})
	}
}

func TestInvalidConfig(t *testing.T) {
	bad := 99

	for _, m := range []*Compress{
		{Encodings: []string{"deflate"}},
		{Encodings: []string{"gzip", "gzip"}},
		{BrotliLevel: &bad},
		{ZstdLevel: &bad},
		{GzipLevel: &bad},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("expected error for %+v", m)
		}
	}
}
