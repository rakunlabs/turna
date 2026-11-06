package folder

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func get(h http.Handler, target, acceptEncoding string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}

	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestAcceptedEncodings(t *testing.T) {
	pref := []string{"br", "zstd", "gzip"}

	for _, tc := range []struct {
		header string
		want   []string
	}{
		{"", []string{}},
		{"gzip", []string{"gzip"}},
		{"gzip, br", []string{"br", "gzip"}},
		{"gzip;q=1, br;q=0.5", []string{"gzip", "br"}},
		{"br;q=0, gzip", []string{"gzip"}},
		{"*", []string{"br", "zstd", "gzip"}},
		{"*;q=0.1, gzip;q=0", []string{"br", "zstd"}},
		{"identity", []string{}},
		{"x-gzip", []string{"gzip"}},
		{"ZSTD;Q=0.8, deflate", []string{"zstd"}},
		{"br;q=abc", []string{}},
		{"br;q=2, gzip;q=0.5", []string{"gzip"}},
		{"br;q=-1, gzip", []string{"gzip"}},
		{" gzip ; q=0.3 , br ; q=0.9", []string{"br", "gzip"}},
	} {
		got := acceptedEncodings([]string{tc.header}, pref)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: got %v want %v", tc.header, got, tc.want)
		}
	}
}

func TestPrecompressed(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"app.js":       "plain-js",
		"app.js.br":    "brotli-js",
		"app.js.gz":    "gzip-js",
		"style.css":    "plain-css",
		"style.css.gz": "gzip-css",
		"data.bin":     "plain-bin",
		"data.bin.gz":  "gzip-bin",
		"only.txt":     "plain-only",
		"arch.tar.gz":  "tarball",
	})

	h := newFolderHandler(t, &Folder{Path: dir, Precompressed: []string{"br", "zstd", "gzip"}})

	for _, tc := range []struct {
		path, accept, body, encoding, ctype string
	}{
		{"/app.js", "gzip, br", "brotli-js", "br", "text/javascript; charset=utf-8"},
		{"/app.js", "gzip", "gzip-js", "gzip", "text/javascript; charset=utf-8"},
		{"/app.js", "br;q=0.5, gzip", "gzip-js", "gzip", "text/javascript; charset=utf-8"},
		{"/app.js", "br;q=0, gzip;q=0", "plain-js", "", "text/javascript; charset=utf-8"},
		{"/app.js", "zstd", "plain-js", "", "text/javascript; charset=utf-8"},
		{"/app.js", "", "plain-js", "", "text/javascript; charset=utf-8"},
		{"/style.css", "br, gzip", "gzip-css", "gzip", "text/css; charset=utf-8"},
		{"/data.bin", "gzip", "gzip-bin", "gzip", "application/octet-stream"},
		{"/only.txt", "br, gzip", "plain-only", "", "text/plain; charset=utf-8"},
	} {
		rec := get(h, tc.path, tc.accept)

		if rec.Code != http.StatusOK || rec.Body.String() != tc.body {
			t.Fatalf("%s %q: status %d body %q, want %q", tc.path, tc.accept, rec.Code, rec.Body.String(), tc.body)
		}

		if got := rec.Header().Get("Content-Encoding"); got != tc.encoding {
			t.Fatalf("%s %q: content-encoding %q want %q", tc.path, tc.accept, got, tc.encoding)
		}

		if got := rec.Header().Get("Content-Type"); got != tc.ctype {
			t.Fatalf("%s %q: content-type %q want %q", tc.path, tc.accept, got, tc.ctype)
		}

		if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
			t.Fatalf("%s %q: vary %q", tc.path, tc.accept, got)
		}

		if rec.Header().Get("Content-Length") != strconv.Itoa(len(tc.body)) {
			t.Fatalf("%s: content-length %s", tc.path, rec.Header().Get("Content-Length"))
		}
	}

	// compressed files themselves are served as is
	rec := get(h, "/arch.tar.gz", "gzip")
	if rec.Body.String() != "tarball" || rec.Header().Get("Content-Encoding") != "" || rec.Header().Get("Vary") != "" {
		t.Fatalf("archive: body %q headers %v", rec.Body.String(), rec.Header())
	}

	// a sidecar is served directly too
	rec = get(h, "/app.js.gz", "")
	if rec.Body.String() != "gzip-js" || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("sidecar: body %q headers %v", rec.Body.String(), rec.Header())
	}
}

func TestPrecompressedDisabled(t *testing.T) {
	dir := writeFiles(t, map[string]string{"app.js": "plain-js", "app.js.br": "brotli-js"})

	h := newFolderHandler(t, &Folder{Path: dir})

	rec := get(h, "/app.js", "br")
	if rec.Body.String() != "plain-js" || rec.Header().Get("Content-Encoding") != "" || rec.Header().Get("Vary") != "" {
		t.Fatalf("body %q headers %v", rec.Body.String(), rec.Header())
	}
}

func TestPrecompressedInvalid(t *testing.T) {
	if _, err := (&Folder{Path: t.TempDir(), Precompressed: []string{"deflate"}}).Middleware(); err == nil {
		t.Fatal("expected error")
	}
}

func TestPrecompressedETag(t *testing.T) {
	dir := writeFiles(t, map[string]string{"app.js": "plain-js", "app.js.br": "brotli-js", "app.js.gz": "gzip-js"})

	for _, mode := range []string{etagWeak, etagStrong} {
		t.Run(mode, func(t *testing.T) {
			h := newFolderHandler(t, &Folder{Path: dir, ETag: mode, Precompressed: []string{"br", "gzip"}})

			plain := get(h, "/app.js", "").Header().Get("ETag")
			br := get(h, "/app.js", "br").Header().Get("ETag")
			gz := get(h, "/app.js", "gzip").Header().Get("ETag")

			if plain == "" || br == plain || gz == plain || br == gz {
				t.Fatalf("etags must differ: %q %q %q", plain, br, gz)
			}

			if !strings.HasSuffix(br, "-br\"") || !strings.HasSuffix(gz, "-gzip\"") {
				t.Fatalf("unexpected etags %q %q", br, gz)
			}

			rec := get(h, "/app.js", "br", "If-None-Match", br)
			if rec.Code != http.StatusNotModified {
				t.Fatalf("status = %d, want 304", rec.Code)
			}

			if rec.Header().Get("Vary") != "Accept-Encoding" {
				t.Fatalf("304 without vary: %v", rec.Header())
			}

			if rec := get(h, "/app.js", "gzip", "If-None-Match", br); rec.Code != http.StatusOK || rec.Body.String() != "gzip-js" {
				t.Fatalf("status = %d body %q, want gzip representation", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPrecompressedSPAAndIndex(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"index.html":        "plain-index",
		"index.html.br":     "brotli-index",
		"sub/index.html":    "plain-sub",
		"sub/index.html.gz": "gzip-sub",
	})

	h := newFolderHandler(t, &Folder{Path: dir, SPA: true, Index: true, Precompressed: []string{"br", "gzip"}})

	for _, tc := range []struct {
		path, accept, body, encoding string
	}{
		{"/some/route", "br", "brotli-index", "br"},
		{"/some/route", "", "plain-index", ""},
		{"/", "br", "brotli-index", "br"},
		{"/sub/", "br, gzip", "gzip-sub", "gzip"},
	} {
		rec := get(h, tc.path, tc.accept)

		if rec.Code != http.StatusOK || rec.Body.String() != tc.body || rec.Header().Get("Content-Encoding") != tc.encoding {
			t.Fatalf("%s %q: status %d body %q headers %v", tc.path, tc.accept, rec.Code, rec.Body.String(), rec.Header())
		}

		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Fatalf("%s: content-type %q", tc.path, got)
		}
	}
}

func TestPrecompressedRange(t *testing.T) {
	dir := writeFiles(t, map[string]string{"app.js": "plain-js", "app.js.br": "0123456789"})

	h := newFolderHandler(t, &Folder{Path: dir, Precompressed: []string{"br"}})

	rec := get(h, "/app.js", "br", "Range", "bytes=2-5")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("content-range %q", got)
	}

	if rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("headers %v", rec.Header())
	}
}

func TestPrecompressedConfigNotMutated(t *testing.T) {
	f := &Folder{Path: t.TempDir(), Precompressed: []string{" BR ", "Gzip"}}
	if _, err := f.Middleware(); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(f.Precompressed, []string{" BR ", "Gzip"}) {
		t.Fatalf("config mutated: %q", f.Precompressed)
	}

	if !reflect.DeepEqual(f.precompressedEnc, []string{"br", "gzip"}) {
		t.Fatalf("normalized %q", f.precompressedEnc)
	}
}

func TestPrecompressedHEADAndSidecarDir(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"app.js":         "plain-js",
		"app.js.br":      "brotli-js",
		"dir.js":         "plain-dir",
		"dir.js.br/file": "x",
	})

	h := newFolderHandler(t, &Folder{Path: dir, Precompressed: []string{"br"}})

	req := httptest.NewRequest(http.MethodHead, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "9" || rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("HEAD: status %d body %q headers %v", rec.Code, rec.Body.String(), rec.Header())
	}

	// a directory named like a sidecar is ignored
	rec = get(h, "/dir.js", "br")
	if rec.Body.String() != "plain-dir" || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("dir sidecar: body %q headers %v", rec.Body.String(), rec.Header())
	}
}
