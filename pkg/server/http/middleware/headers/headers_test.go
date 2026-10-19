package headers

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestHeaders(t *testing.T) {
	m := &Headers{
		CustomRequestHeaders:  map[string]string{"X-Req": "set", "X-Drop": ""},
		AppendRequestHeaders:  map[string]string{"X-Multi": "b"},
		CustomResponseHeaders: map[string]string{"X-Frame-Options": "DENY", "Server": "", "X-Up": "override"},
		AppendResponseHeaders: map[string]string{"Vary": "Origin"},
	}

	var got http.Header
	h := m.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()

		// headers written by the upstream
		w.Header().Set("Server", "nginx")
		w.Header().Set("X-Up", "upstream")
		w.Header().Set("Vary", "Accept-Encoding")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Drop", "x")
	req.Header.Set("X-Multi", "a")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got.Get("X-Req") != "set" || got.Get("X-Drop") != "" || !slices.Equal(got.Values("X-Multi"), []string{"a", "b"}) {
		t.Fatalf("request headers = %v", got)
	}

	res := rec.Result().Header
	if res.Get("X-Frame-Options") != "DENY" || res.Get("Server") != "" || res.Get("X-Up") != "override" {
		t.Fatalf("response headers = %v", res)
	}

	if !slices.Equal(res.Values("Vary"), []string{"Accept-Encoding", "Origin"}) {
		t.Fatalf("vary = %v", res.Values("Vary"))
	}
}

func TestHeadersNoWrite(t *testing.T) {
	m := &Headers{CustomResponseHeaders: map[string]string{"X-A": "1"}}

	rec := httptest.NewRecorder()
	m.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Result().Header.Get("X-A") != "1" {
		t.Fatalf("headers = %v", rec.Result().Header)
	}
}
