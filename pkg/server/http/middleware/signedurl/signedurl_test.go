package signedurl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/folder"
)

const secret = "0123456789abcdef-secret"

var now = time.Unix(1_800_000_000, 0)

func setup(t *testing.T, m *SignedURL) (http.Handler, *signer, *string) {
	t.Helper()

	s, err := m.build()
	if err != nil {
		t.Fatal(err)
	}

	s.now = func() time.Time { return now }

	got := new(string)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = r.RequestURI
		w.WriteHeader(http.StatusOK)
	})

	return s.middleware(h), s, got
}

func get(h http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	return rec
}

func sign(t *testing.T, s *signer, path string, ttl time.Duration) string {
	t.Helper()

	u, err := s.Sign(path, ttl)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

func TestValid(t *testing.T) {
	h, s, got := setup(t, &SignedURL{Secrets: []string{secret}})

	u := sign(t, s, "/files/report.pdf?download=1", time.Hour)
	if rec := get(h, u); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	if *got != "/files/report.pdf?download=1" {
		t.Fatalf("upstream got %q", *got)
	}
}

func TestRejected(t *testing.T) {
	h, s, _ := setup(t, &SignedURL{Secrets: []string{secret}, MaxTTL: 24 * time.Hour})

	valid := sign(t, s, "/files/a.pdf?x=1", time.Hour)

	cases := map[string]string{
		"missing":       "/files/a.pdf",
		"other path":    strings.Replace(valid, "a.pdf", "b.pdf", 1),
		"changed query": strings.Replace(valid, "x=1", "x=2", 1),
		"added query":   valid + "&y=1",
		"expired":       sign(t, s, "/files/a.pdf", -time.Second),
		"too long":      sign(t, s, "/files/a.pdf", 48*time.Hour),
		"bad signature": valid[:len(valid)-2] + "xx",
	}

	other, _ := (&SignedURL{Secrets: []string{"another-secret-of-16b"}}).build()
	other.now = s.now
	cases["other secret"] = sign(t, other, "/files/a.pdf", time.Hour)

	for name, target := range cases {
		if rec := get(h, target); rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d", name, rec.Code)
		}
	}
}

func TestRotation(t *testing.T) {
	old, _ := (&SignedURL{Secrets: []string{"old-secret-0123456789"}}).build()
	old.now = func() time.Time { return now }

	h, _, _ := setup(t, &SignedURL{Secrets: []string{secret, "old-secret-0123456789"}})

	if rec := get(h, sign(t, old, "/a", time.Hour)); rec.Code != http.StatusOK {
		t.Fatalf("old secret status = %d", rec.Code)
	}
}

func TestUnsignedQuery(t *testing.T) {
	f := false
	h, s, got := setup(t, &SignedURL{Secrets: []string{secret}, SignQuery: &f, StripParams: &f})

	u := sign(t, s, "/a", time.Hour) + "&page=2"
	if rec := get(h, u); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	if !strings.Contains(*got, "signature=") {
		t.Fatalf("params stripped: %q", *got)
	}
}

func TestPathPrefix(t *testing.T) {
	h, s, _ := setup(t, &SignedURL{Secrets: []string{secret}, PathPrefix: true, SignQuery: new(bool)})

	u, _ := url.Parse(sign(t, s, "/share/folder/", time.Hour))

	for path, want := range map[string]int{
		"/share/folder/":          http.StatusOK,
		"/share/folder/a.txt":     http.StatusOK,
		"/share/folder/sub/b.txt": http.StatusOK,
		"/share/other/a.txt":      http.StatusForbidden,
		"/share/folderx/a.txt":    http.StatusForbidden,
	} {
		u.Path = path
		if rec := get(h, u.String()); rec.Code != want {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
	}
}

func TestSignPath(t *testing.T) {
	h, _, _ := setup(t, &SignedURL{Secrets: []string{secret}, SignPath: "/_sign", MaxTTL: 2 * time.Hour})

	rec := get(h, "/_sign?path="+url.QueryEscape("/files/a.pdf?v=1")+"&ttl=30m")
	if rec.Code != http.StatusOK {
		t.Fatalf("sign status = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		URL     string `json:"url"`
		Expires int64  `json:"expires"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Expires != now.Add(30*time.Minute).Unix() {
		t.Fatalf("expires = %d", resp.Expires)
	}

	if rec := get(h, resp.URL); rec.Code != http.StatusOK {
		t.Fatalf("signed url status = %d", rec.Code)
	}

	if rec := get(h, "/_sign?path=/a&ttl=3h"); rec.Code != http.StatusBadRequest {
		t.Fatalf("over max ttl status = %d", rec.Code)
	}
}

func TestTraversal(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"share/folder/a.txt": "public", "share/private/s.txt": "SECRET"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	s, err := (&SignedURL{Secrets: []string{secret}, PathPrefix: true}).build()
	if err != nil {
		t.Fatal(err)
	}

	files, err := (&folder.Folder{Path: dir}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := s.middleware(files(http.NotFoundHandler()))

	u, _ := url.Parse(sign(t, s, "/share/folder/", time.Hour))

	for p, want := range map[string]int{
		"/share/folder/a.txt":                   http.StatusOK,
		"/share/folder/../private/s.txt":        http.StatusForbidden,
		"/share/folder/%2e%2e/private/s.txt":    http.StatusForbidden,
		"/share/folder//../private/s.txt":       http.StatusForbidden,
		"/share/folder/./../private/s.txt":      http.StatusForbidden,
		"/share/folder/sub/../../private/s.txt": http.StatusForbidden,
	} {
		target := p + "?" + u.RawQuery

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if req.URL, err = url.ParseRequestURI(target); err != nil {
			t.Fatal(err)
		}
		req.RequestURI = target

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != want || strings.Contains(rec.Body.String(), "SECRET") {
			t.Fatalf("%s: status = %d body = %q", p, rec.Code, rec.Body.String())
		}
	}
}

func TestSignCleansPath(t *testing.T) {
	h, s, got := setup(t, &SignedURL{Secrets: []string{secret}})

	if rec := get(h, sign(t, s, "/a/../b", time.Hour)); rec.Code != http.StatusOK || *got != "/b" {
		t.Fatalf("status = %d upstream %q", rec.Code, *got)
	}
}

func TestConfig(t *testing.T) {
	for _, m := range []*SignedURL{
		{},
		{Secrets: []string{"short"}},
		{Secrets: []string{secret}, ExpiresParam: "s", SignatureParam: "s"},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v accepted", m)
		}
	}
}

func TestMiddleware(t *testing.T) {
	mw, err := (&SignedURL{Secrets: []string{secret}}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	var uri string
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { uri = r.RequestURI }))

	s, _ := (&SignedURL{Secrets: []string{secret}}).build()

	if rec := get(h, sign(t, s, "/a?b=1", time.Minute)); rec.Code != http.StatusOK || uri != "/a?b=1" {
		t.Fatalf("status = %d uri = %q", rec.Code, uri)
	}
}
