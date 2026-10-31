package whoami

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T, m *Whoami) http.Handler {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called")
	}))
}

func do(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-Test", "value")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestText(t *testing.T) {
	t.Setenv("WHOAMI_TEST", "secret")

	h := setup(t, &Whoami{Name: "turna"})

	rec := do(h, http.MethodPost, "/foo?env=true", "hello")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	got := rec.Body.String()
	for _, want := range []string{"Name: turna\n", "Hostname: ", "POST /foo?env=true HTTP/1.1\n", "X-Test: value\n", "\nhello\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}

	if strings.Contains(got, "WHOAMI_TEST=secret") {
		t.Fatal("environment printed while env is disabled")
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("WHOAMI_TEST", "secret")

	h := setup(t, &Whoami{Env: true})

	if got := do(h, http.MethodGet, "/", "").Body.String(); strings.Contains(got, "WHOAMI_TEST=secret") {
		t.Fatal("environment printed without ?env=true")
	}

	if got := do(h, http.MethodGet, "/?env=true", "").Body.String(); !strings.Contains(got, "WHOAMI_TEST=secret") {
		t.Fatalf("environment missing in\n%s", got)
	}
}

func TestAPI(t *testing.T) {
	h := setup(t, &Whoami{Name: "turna"})

	rec := do(h, http.MethodGet, "/api", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var resp apiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Name != "turna" || resp.Method != http.MethodGet || resp.URL != "/api" || resp.Headers["X-Test"][0] != "value" {
		t.Fatalf("unexpected response %+v", resp)
	}
}

func TestPrefixedPaths(t *testing.T) {
	h := setup(t, &Whoami{})

	for _, target := range []string{"/api", "/app/api", "/a/b/api/"} {
		rec := do(h, http.MethodGet, target, "")
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("%s: content type = %q", target, ct)
		}
	}

	if rec := do(h, http.MethodGet, "/app/bench", ""); rec.Body.String() != "1" {
		t.Fatalf("bench body = %q", rec.Body.String())
	}

	if rec := do(h, http.MethodGet, "/app/apix", ""); !strings.Contains(rec.Body.String(), "GET /app/apix") {
		t.Fatalf("expected text output, got\n%s", rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	h := setup(t, &Whoami{})

	if rec := do(h, http.MethodGet, "/health", ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	if rec := do(h, http.MethodPost, "/health", "503"); rec.Code != http.StatusOK {
		t.Fatalf("set status = %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/health", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}

	for _, bad := range []string{"abc", "101", "103", "600", "999"} {
		if rec := do(h, http.MethodPost, "/health", bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", bad, rec.Code)
		}
	}
}

func TestData(t *testing.T) {
	h := setup(t, &Whoami{MaxDataSize: 2 << 20})

	rec := do(h, http.MethodGet, "/data?size=1&unit=MB&attachment=true", "")
	if rec.Code != http.StatusOK || rec.Body.Len() != 1<<20 {
		t.Fatalf("status = %d, len = %d", rec.Code, rec.Body.Len())
	}

	if rec.Header().Get("Content-Disposition") == "" {
		t.Fatal("missing content disposition")
	}

	if rec := do(h, http.MethodGet, "/data?size=3&unit=MB", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("over limit status = %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/data?unit=TB", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid unit status = %d", rec.Code)
	}
}

func TestWait(t *testing.T) {
	h := setup(t, &Whoami{MaxWait: 10 * time.Millisecond})

	start := time.Now()
	if rec := do(h, http.MethodGet, "/bench?wait=1h", ""); rec.Code != http.StatusOK || rec.Body.String() != "1" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}

	if time.Since(start) > time.Second {
		t.Fatal("max_wait not applied")
	}

	if rec := do(h, http.MethodGet, "/?wait=abc", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid wait status = %d", rec.Code)
	}

	m := &Whoami{}
	if _, err := m.Middleware(); err != nil || m.MaxWait != 30*time.Second {
		t.Fatalf("default max_wait = %s %v", m.MaxWait, err)
	}

	if _, err := (&Whoami{MaxWait: -time.Second}).Middleware(); err == nil {
		t.Fatal("negative max_wait accepted")
	}
}
