package decompress

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func encode(t *testing.T, enc string, data string) []byte {
	t.Helper()

	var buf bytes.Buffer
	var w io.WriteCloser

	switch enc {
	case "gzip":
		w = gzip.NewWriter(&buf)
	case "br":
		w = brotli.NewWriter(&buf)
	case "zstd":
		zw, err := zstd.NewWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		w = zw
	}

	_, _ = io.WriteString(w, data)
	_ = w.Close()

	return buf.Bytes()
}

func TestDecompress(t *testing.T) {
	h := (&Decompress{}).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "" {
			t.Error("Content-Encoding must be removed")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		_, _ = w.Write(body)
	}))

	for _, enc := range []string{"gzip", "br", "zstd"} {
		t.Run(enc, func(t *testing.T) {
			// run twice to use pooled readers
			for range 2 {
				req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(encode(t, enc, "hello turna")))
				req.Header.Set("Content-Encoding", enc)

				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)

				if rec.Body.String() != "hello turna" {
					t.Fatalf("body = %q", rec.Body.String())
				}
			}
		})
	}
}

func TestDecompressInvalid(t *testing.T) {
	h := (&Decompress{}).Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
		}
	}))

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("not gzip data")))
	req.Header.Set("Content-Encoding", "gzip")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}
