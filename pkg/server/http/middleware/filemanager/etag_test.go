package filemanager

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestETagModes(t *testing.T) {
	for _, mode := range []string{"", etagWeak, etagStrong, etagHash} {
		t.Run("mode="+mode, func(t *testing.T) {
			h, dir := newHandler(t, &FileManager{ETag: mode})
			if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("a: 1\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			rec := do(h, http.MethodGet, "/raw/a.yaml", nil)
			tag := rec.Header().Get("ETag")
			if tag == "" {
				t.Fatal("etag is empty")
			}

			if weak := strings.HasPrefix(tag, "W/"); weak != (mode == "" || mode == etagWeak) {
				t.Fatalf("etag %q for mode %q", tag, mode)
			}

			if stat := do(h, http.MethodGet, "/api/stat?path=/a.yaml", nil); stat.Header().Get("ETag") != tag {
				t.Fatalf("stat etag = %q, raw etag = %q", stat.Header().Get("ETag"), tag)
			}

			if rec := do(h, http.MethodGet, "/raw/a.yaml", nil, "If-None-Match", tag); rec.Code != http.StatusNotModified {
				t.Fatalf("conditional get status = %d", rec.Code)
			}

			// a proxy may weaken or strip the W/ prefix; both still match
			other := "W/" + strings.TrimPrefix(tag, "W/")
			if strings.HasPrefix(tag, "W/") {
				other = strings.TrimPrefix(tag, "W/")
			}

			// different size so the weak tag changes even within one filesystem timestamp tick
			rec = do(h, http.MethodPut, "/api/file?path=/a.yaml", []byte("a: 22\n"), "If-Match", `"nope", `+other)
			if rec.Code != http.StatusOK {
				t.Fatalf("write with list status = %d body = %s", rec.Code, rec.Body.String())
			}

			next := rec.Header().Get("ETag")
			if next == "" || next == tag {
				t.Fatalf("etag did not change: %q -> %q", tag, next)
			}

			if rec := do(h, http.MethodPut, "/api/file?path=/a.yaml", []byte("a: 3\n"), "If-Match", tag); rec.Code != http.StatusPreconditionFailed {
				t.Fatalf("stale write status = %d", rec.Code)
			}
		})
	}
}

func TestETagHashFollowsContent(t *testing.T) {
	h, dir := newHandler(t, &FileManager{ETag: etagHash})
	file := filepath.Join(dir, "same.txt")

	if err := os.WriteFile(file, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}

	first := do(h, http.MethodGet, "/api/stat?path=/same.txt", nil).Header().Get("ETag")

	// the same bytes written again keep the hash even though modtime moved
	rec := do(h, http.MethodPut, "/api/file?path=/same.txt", []byte("same"), "If-Match", first)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status = %d", rec.Code)
	}

	if got := rec.Header().Get("ETag"); got != first {
		t.Fatalf("hash etag changed for identical content: %q -> %q", first, got)
	}

	if rec := do(h, http.MethodPut, "/api/file?path=/same.txt", []byte("other"), "If-Match", first); rec.Code != http.StatusOK {
		t.Fatalf("second write status = %d", rec.Code)
	}

	if got := do(h, http.MethodGet, "/api/stat?path=/same.txt", nil).Header().Get("ETag"); got == first {
		t.Fatal("hash etag did not change with content")
	}
}

func TestETagPreconditions(t *testing.T) {
	h, _ := newHandler(t, &FileManager{})

	if rec := do(h, http.MethodPut, "/api/file?path=/n.json", []byte(`{}`), "If-Match", "*"); rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("If-Match * on missing file status = %d", rec.Code)
	}

	if rec := do(h, http.MethodPut, "/api/file?path=/n.json", []byte(`{}`), "If-None-Match", "*"); rec.Code != http.StatusOK {
		t.Fatalf("If-None-Match * on missing file status = %d", rec.Code)
	}

	if rec := do(h, http.MethodPut, "/api/file?path=/n.json", []byte(`{}`), "If-None-Match", "*"); rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("If-None-Match * on existing file status = %d", rec.Code)
	}

	if rec := do(h, http.MethodPut, "/api/file?path=/n.json", []byte(`{"v":1}`), "If-Match", "*"); rec.Code != http.StatusOK {
		t.Fatalf("If-Match * on existing file status = %d", rec.Code)
	}
}

func TestETagInvalidMode(t *testing.T) {
	if _, err := (&FileManager{Path: t.TempDir(), ETag: "nope"}).Middleware(); err == nil {
		t.Fatal("expected error for unknown etag mode")
	}

	if _, err := (&FileManager{Path: t.TempDir(), ETagRules: []ETagRule{{Pattern: "**/*.mp4", ETag: "nope"}}}).Middleware(); err == nil {
		t.Fatal("expected error for unknown rule mode")
	}

	if _, err := (&FileManager{Path: t.TempDir(), ETagRules: []ETagRule{{Pattern: "[", ETag: etagStrong}}}).Middleware(); err == nil {
		t.Fatal("expected error for bad pattern")
	}
}

func TestMatchETag(t *testing.T) {
	cases := []struct {
		header, current string
		want            bool
	}{
		{`"a"`, `"a"`, true},
		{`W/"a"`, `"a"`, true},
		{`"a"`, `W/"a"`, true},
		{`"b", W/"a"`, `"a"`, true},
		{`"b"`, `"a"`, false},
		{`*`, `"a"`, true},
		{`*`, ``, false},
		{`"a"`, ``, false},
	}

	for _, c := range cases {
		if got := matchETag(c.header, c.current); got != c.want {
			t.Errorf("matchETag(%q, %q) = %v, want %v", c.header, c.current, got, c.want)
		}
	}
}

func TestETagRules(t *testing.T) {
	h, dir := newHandler(t, &FileManager{
		ETagRules: []ETagRule{
			{Pattern: "/videos/**", ETag: etagStrong},
			{Pattern: "**/*.json", ETag: etagHash},
		},
	})

	for _, f := range []string{"videos/a.mp4", "conf/a.json", "a.txt"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, f), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stat := func(p string) (tag, mode string) {
		rec := do(h, http.MethodGet, "/api/stat?path=/"+p, nil)

		var e entry
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
			t.Fatal(err)
		}

		if e.ETag != rec.Header().Get("ETag") {
			t.Fatalf("%s: body etag %q, header %q", p, e.ETag, rec.Header().Get("ETag"))
		}

		return e.ETag, e.ETagMode
	}

	if tag, mode := stat("videos/a.mp4"); mode != etagStrong || strings.HasPrefix(tag, "W/") || !strings.Contains(tag, "-") {
		t.Fatalf("video: %q %q", tag, mode)
	}

	if tag, mode := stat("conf/a.json"); mode != etagHash || strings.HasPrefix(tag, "W/") || strings.Contains(tag, "-a\"") {
		t.Fatalf("json: %q %q", tag, mode)
	}

	if tag, mode := stat("a.txt"); mode != etagWeak || !strings.HasPrefix(tag, "W/") {
		t.Fatalf("txt: %q %q", tag, mode)
	}
}

// Players resume with If-Range; only a strong tag lets the server answer with a range.
func TestETagIfRange(t *testing.T) {
	for _, c := range []struct {
		mode string
		want int
	}{
		{etagWeak, http.StatusOK},
		{etagStrong, http.StatusPartialContent},
		{etagHash, http.StatusPartialContent},
	} {
		h, dir := newHandler(t, &FileManager{ETag: c.mode})
		if err := os.WriteFile(filepath.Join(dir, "v.mp4"), []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}

		tag := do(h, http.MethodGet, "/raw/v.mp4", nil).Header().Get("ETag")

		rec := do(h, http.MethodGet, "/raw/v.mp4", nil, "Range", "bytes=4-6", "If-Range", tag)
		if rec.Code != c.want {
			t.Fatalf("%s: If-Range status = %d, want %d", c.mode, rec.Code, c.want)
		}

		if c.want == http.StatusPartialContent && rec.Body.String() != "456" {
			t.Fatalf("%s: body = %q", c.mode, rec.Body.String())
		}
	}
}
