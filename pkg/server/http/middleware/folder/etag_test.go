package folder

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func newFolderHandler(t *testing.T, f *Folder) http.Handler {
	t.Helper()

	m, err := f.Middleware()
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	return m(nil)
}

func TestETag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{etagWeak, etagStrong} {
		t.Run(mode, func(t *testing.T) {
			h := newFolderHandler(t, &Folder{Path: dir, ETag: mode})

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a.txt", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}

			etag := rec.Header().Get("ETag")
			if etag == "" {
				t.Fatal("etag is empty")
			}

			if weak := strings.HasPrefix(etag, "W/"); weak != (mode == etagWeak) {
				t.Fatalf("unexpected etag %q for mode %s", etag, mode)
			}

			req := httptest.NewRequest(http.MethodGet, "/a.txt", nil)
			req.Header.Set("If-None-Match", etag)
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotModified {
				t.Fatalf("status = %d, want 304", rec.Code)
			}

			req = httptest.NewRequest(http.MethodGet, "/a.txt", nil)
			req.Header.Set("If-None-Match", `"other"`)
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
				t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestETagStrongEmbedded(t *testing.T) {
	f := &Folder{ETag: etagStrong}
	f.SetFs(http.FS(fstest.MapFS{"a.txt": {Data: []byte("hello")}}))

	h := newFolderHandler(t, f)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a.txt", nil))

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("strong etag must work without modtime")
	}

	req := httptest.NewRequest(http.MethodGet, "/a.txt", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
}

func TestETagChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newFolderHandler(t, &Folder{Path: dir, ETag: etagStrong})

	get := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a.txt", nil))

		return rec.Header().Get("ETag")
	}

	first := get()

	if err := os.WriteFile(p, []byte("world!"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(p, time.Now().Add(time.Hour), time.Now().Add(time.Hour))

	if second := get(); second == first {
		t.Fatalf("etag did not change: %s", first)
	}
}

func TestETagInvalidMode(t *testing.T) {
	if _, err := (&Folder{ETag: "nope"}).Middleware(); err == nil {
		t.Fatal("expected error")
	}
}
