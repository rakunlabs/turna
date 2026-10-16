package filemanager

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newHandler(t *testing.T, m *FileManager) (http.Handler, string) {
	t.Helper()

	if m.Path == "" {
		m.Path = t.TempDir()
	}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	return mw(nil), m.Path
}

func do(h http.Handler, method, target string, body []byte, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestRawServe(t *testing.T) {
	h, dir := newHandler(t, &FileManager{})

	if err := os.MkdirAll(filepath.Join(dir, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "configs", "app.json"), []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "configs", "app.yaml"), []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := do(h, http.MethodGet, "/raw/configs/app.json", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"a":1}` {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}

	etag := rec.Header().Get("ETag")
	if rec := do(h, http.MethodGet, "/raw/configs/app.json", nil, "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/raw/configs/app.yaml", nil); rec.Header().Get("Content-Type") != "application/yaml" {
		t.Fatalf("yaml content-type = %q", rec.Header().Get("Content-Type"))
	}

	if csp := rec.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Fatalf("csp = %q", csp)
	}

	rec = do(h, http.MethodGet, "/raw/configs/app.json", nil, "Range", "bytes=0-2")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != `{"a` {
		t.Fatalf("range status = %d body = %q", rec.Code, rec.Body.String())
	}

	for _, p := range []string{"/raw/configs", "/raw/missing.json", "/raw/../etc/passwd", "/raw/configs/%2e%2e/%2e%2e/etc/passwd"} {
		if rec := do(h, http.MethodGet, p, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", p, rec.Code)
		}
	}

	if rec := do(h, http.MethodPut, "/raw/configs/app.json", []byte("x")); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("raw put status = %d", rec.Code)
	}
}

func TestSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}

	h, dir := newHandler(t, &FileManager{})
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "link")); err != nil {
		t.Skip("symlink not supported")
	}

	if rec := do(h, http.MethodGet, "/raw/link", nil); rec.Code == http.StatusOK {
		t.Fatalf("symlink escape served the file")
	}
}

func TestPrefix(t *testing.T) {
	h, dir := newHandler(t, &FileManager{PrefixPath: "/files/"})

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	if rec := do(h, http.MethodGet, "/files/raw/a.txt", nil); rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	if rec := do(h, http.MethodGet, "/other/raw/a.txt", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("outside prefix status = %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/files", nil); rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/files/" {
		t.Fatalf("ui redirect status = %d location = %q", rec.Code, rec.Header().Get("Location"))
	}

	rec := do(h, http.MethodGet, "/files/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<div id=\"app\">") {
		t.Fatalf("ui status = %d", rec.Code)
	}

	rec = do(h, http.MethodGet, "/files/api/info", nil)

	var info infoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || info.Prefix != "/files" {
		t.Fatalf("info = %s err = %v", rec.Body.String(), err)
	}
}

func TestAPI(t *testing.T) {
	h, dir := newHandler(t, &FileManager{})

	// create
	rec := do(h, http.MethodPut, "/api/file?path=/a/b/c.json&create", []byte(`{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}

	etag := rec.Header().Get("ETag")

	if rec := do(h, http.MethodPut, "/api/file?path=/a/b/c.json&create", []byte(`{}`)); rec.Code != http.StatusConflict {
		t.Fatalf("create existing status = %d", rec.Code)
	}

	// optimistic write
	rec = do(h, http.MethodPut, "/api/file?path=/a/b/c.json", []byte(`{"v":2}`), "If-Match", etag)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status = %d body = %s", rec.Code, rec.Body.String())
	}

	if rec := do(h, http.MethodPut, "/api/file?path=/a/b/c.json", []byte(`{"v":3}`), "If-Match", etag); rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale write status = %d", rec.Code)
	}

	if b, _ := os.ReadFile(filepath.Join(dir, "a", "b", "c.json")); string(b) != `{"v":2}` {
		t.Fatalf("content = %q", b)
	}

	// list
	rec = do(h, http.MethodGet, "/api/list?path=/a", nil)

	var list struct {
		Entries []entry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Entries) != 1 || !list.Entries[0].Dir || list.Entries[0].Path != "/a/b" {
		t.Fatalf("list = %s", rec.Body.String())
	}

	// mkdir + move into folder
	if rec := do(h, http.MethodPost, "/api/mkdir", []byte(`{"path":"/x"}`)); rec.Code != http.StatusCreated {
		t.Fatalf("mkdir status = %d", rec.Code)
	}

	if rec := do(h, http.MethodPost, "/api/move", []byte(`{"from":"/a/b/c.json","to":"/x/c.json"}`)); rec.Code != http.StatusOK {
		t.Fatalf("move status = %d body = %s", rec.Code, rec.Body.String())
	}

	if rec := do(h, http.MethodPost, "/api/move", []byte(`{"from":"/a","to":"/a/b/a"}`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("move into itself status = %d", rec.Code)
	}

	if rec := do(h, http.MethodPost, "/api/move", []byte(`{"from":"/x/c.json","to":"/../../c.json"}`)); rec.Code != http.StatusOK {
		t.Fatalf("clamped move status = %d", rec.Code)
	}

	if _, err := os.Stat(filepath.Join(dir, "c.json")); err != nil {
		t.Fatalf("clamped move did not stay in root: %v", err)
	}

	// raw read reflects the move
	if rec := do(h, http.MethodGet, "/raw/c.json", nil); rec.Body.String() != `{"v":2}` {
		t.Fatalf("raw = %q", rec.Body.String())
	}

	// upload with folder structure
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "sub/u.yaml")
	_, _ = fw.Write([]byte("k: v\n"))
	_ = mw.Close()

	rec = do(h, http.MethodPost, "/api/upload?path=/x", buf.Bytes(), "Content-Type", mw.FormDataContentType())
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d body = %s", rec.Code, rec.Body.String())
	}

	if b, _ := os.ReadFile(filepath.Join(dir, "x", "sub", "u.yaml")); string(b) != "k: v\n" {
		t.Fatalf("uploaded = %q", b)
	}

	// delete
	if rec := do(h, http.MethodDelete, "/api/file?path=/x", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}

	if rec := do(h, http.MethodDelete, "/api/file?path=/", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("delete root status = %d", rec.Code)
	}

	if _, err := os.Stat(filepath.Join(dir, "x")); !os.IsNotExist(err) {
		t.Fatalf("folder still exists")
	}
}

func TestReadOnly(t *testing.T) {
	h, dir := newHandler(t, &FileManager{ReadOnly: true})

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}

	if rec := do(h, http.MethodPut, "/api/file?path=/a.txt", []byte("b")); rec.Code != http.StatusForbidden {
		t.Fatalf("write status = %d", rec.Code)
	}

	if rec := do(h, http.MethodDelete, "/api/file?path=/a.txt", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("delete status = %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/api/file?path=/a.txt", nil); rec.Code != http.StatusOK {
		t.Fatalf("read status = %d", rec.Code)
	}
}

func TestDisableUI(t *testing.T) {
	h, dir := newHandler(t, &FileManager{DisableUI: true})

	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if rec := do(h, http.MethodGet, "/raw/x", nil); rec.Body.String() != "x" {
		t.Fatalf("body = %q", rec.Body.String())
	}

	for _, p := range []string{"/", "/api/info"} {
		if rec := do(h, http.MethodGet, p, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", p, rec.Code)
		}
	}
}

// A file named like a UI route is still reachable through /raw.
func TestRawNamespace(t *testing.T) {
	h, dir := newHandler(t, &FileManager{})

	if err := os.MkdirAll(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "api", "info"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	if rec := do(h, http.MethodGet, "/raw/api/info", nil); rec.Body.String() != "file" {
		t.Fatalf("raw body = %q", rec.Body.String())
	}

	if rec := do(h, http.MethodGet, "/api/info", nil); !strings.Contains(rec.Body.String(), `"raw_path":"/raw"`) {
		t.Fatalf("api body = %q", rec.Body.String())
	}
}

func TestMaxUploadSize(t *testing.T) {
	h, _ := newHandler(t, &FileManager{MaxUploadSize: 4})

	if rec := do(h, http.MethodPut, "/api/file?path=/a.txt", []byte("too large")); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
}
