package filemanager

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
)

func newGCSHandler(t *testing.T, prefix string) (http.Handler, *storage.BucketHandle) {
	t.Helper()

	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{NoListener: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)

	server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: "files"})
	bucket := server.Client().Bucket("files")

	m := &FileManager{Backend: backendGCS, store: newGCSBackend(bucket, prefix)}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(nil), bucket
}

func readObject(t *testing.T, bucket *storage.BucketHandle, key string) (string, bool) {
	t.Helper()

	r, err := bucket.Object(key).NewReader(t.Context())
	if err != nil {
		return "", false
	}
	defer func() { _ = r.Close() }()

	b, _ := io.ReadAll(r)

	return string(b), true
}

func listNames(t *testing.T, h http.Handler, dir string) map[string]bool {
	t.Helper()

	rec := do(h, http.MethodGet, "/api/list?path="+dir, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list %s: status = %d body = %s", dir, rec.Code, rec.Body.String())
	}

	var res struct {
		Entries []entry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}

	names := map[string]bool{}
	for _, e := range res.Entries {
		names[e.Name] = e.Dir
	}

	return names
}

func TestGCS(t *testing.T) {
	h, bucket := newGCSHandler(t, "/data/")

	if rec := do(h, http.MethodPut, "/api/file?path=/a/b/c.json", []byte(`{"v":1}`)); rec.Code != http.StatusOK {
		t.Fatalf("write: status = %d body = %s", rec.Code, rec.Body.String())
	}

	if v, ok := readObject(t, bucket, "data/a/b/c.json"); !ok || v != `{"v":1}` {
		t.Fatalf("object = %q, %v", v, ok)
	}

	rec := do(h, http.MethodGet, "/raw/a/b/c.json", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"v":1}` || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("raw: status = %d body = %q ct = %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}

	etag := rec.Header().Get("ETag")
	if rec := do(h, http.MethodGet, "/raw/a/b/c.json", nil, "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/raw/a/b/c.json", nil, "Range", "bytes=2-4"); rec.Code != http.StatusPartialContent || rec.Body.String() != `v":` {
		t.Fatalf("range: status = %d body = %q", rec.Code, rec.Body.String())
	}

	if rec := do(h, http.MethodPut, "/api/file?path=/a/b/c.json", []byte(`{"v":2}`), "If-Match", `"stale"`); rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale write: status = %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/raw/a/b", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("raw folder: status = %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/api/stat?path=/a", nil); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"dir":true`)) {
		t.Fatalf("stat folder: status = %d body = %s", rec.Code, rec.Body.String())
	}

	if rec := do(h, http.MethodPut, "/api/file?path=/a/b/c.json/x", []byte("x")); rec.Code != http.StatusConflict {
		t.Fatalf("write below a file: status = %d", rec.Code)
	}

	// an empty folder stays visible through its marker
	if rec := do(h, http.MethodPost, "/api/mkdir", []byte(`{"path":"/empty"}`)); rec.Code != http.StatusCreated {
		t.Fatalf("mkdir: status = %d body = %s", rec.Code, rec.Body.String())
	}

	if names := listNames(t, h, "/"); len(names) != 2 || !names["a"] || !names["empty"] {
		t.Fatalf("root entries = %v", names)
	}

	if names := listNames(t, h, "/empty"); len(names) != 0 {
		t.Fatalf("empty entries = %v", names)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "sub/u.yaml")
	_, _ = fw.Write([]byte("k: v\n"))
	_ = mw.Close()

	if rec := do(h, http.MethodPost, "/api/upload?path=/a", body.Bytes(), "Content-Type", mw.FormDataContentType()); rec.Code != http.StatusOK {
		t.Fatalf("upload: status = %d body = %s", rec.Code, rec.Body.String())
	}

	if rec := do(h, http.MethodPost, "/api/move", []byte(`{"from":"/a","to":"/empty/moved"}`)); rec.Code != http.StatusOK {
		t.Fatalf("move: status = %d body = %s", rec.Code, rec.Body.String())
	}

	if _, ok := readObject(t, bucket, "data/a/b/c.json"); ok {
		t.Fatal("source object still exists")
	}

	if v, _ := readObject(t, bucket, "data/empty/moved/sub/u.yaml"); v != "k: v\n" {
		t.Fatalf("moved upload = %q", v)
	}

	if names := listNames(t, h, "/empty/moved"); len(names) != 2 || !names["b"] || !names["sub"] {
		t.Fatalf("moved entries = %v", names)
	}

	if rec := do(h, http.MethodDelete, "/api/file?path=/empty", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", rec.Code)
	}

	if names := listNames(t, h, "/"); len(names) != 0 {
		t.Fatalf("root after delete = %v", names)
	}

	if rec := do(h, http.MethodGet, "/api/stat?path=/empty", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("stat deleted: status = %d", rec.Code)
	}
}

func TestGCSConfig(t *testing.T) {
	if _, err := (&FileManager{Backend: backendGCS}).Middleware(); err == nil {
		t.Fatal("missing bucket accepted")
	}

	if _, err := (&FileManager{Backend: "s3", Path: t.TempDir()}).Middleware(); err == nil {
		t.Fatal("unknown backend accepted")
	}
}
