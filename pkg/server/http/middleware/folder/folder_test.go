package folder

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStripIndexNameRedirectStaysLocal(t *testing.T) {
	dir := writeFiles(t, map[string]string{"index.html": "x"})
	h := newFolderHandler(t, &Folder{Path: dir, StripIndexName: true})

	for _, p := range []string{"//evil.com/index.html", "/index.html", "/sub/index.html"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://local"+p, nil))

		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("%s: status = %d", p, rec.Code)
		}

		if loc := rec.Header().Get("Location"); loc != "./" {
			t.Fatalf("%s: location = %q", p, loc)
		}
	}
}

func TestDirListEscapesNames(t *testing.T) {
	name := `x" onmouseover="alert(1)`
	dir := writeFiles(t, map[string]string{name: "x"})
	h := newFolderHandler(t, &Folder{Path: dir, Browse: true})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, `href="./x"`) || !strings.Contains(body, `href="./x%22%20onmouseover`) {
		t.Fatalf("unescaped name in href: %s", body)
	}
}
