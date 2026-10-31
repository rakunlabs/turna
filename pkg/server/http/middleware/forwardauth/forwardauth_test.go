package forwardauth

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type seen struct {
	header http.Header
	method string
	body   string
}

func authServer(t *testing.T, got *seen) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = seen{header: r.Header.Clone(), method: r.Method, body: string(b)}

		switch r.Header.Get("Authorization") {
		case "Bearer good":
			w.Header().Set("X-User", "alice")
			w.Header().Set("X-Auth-Groups", "admin")
			w.Header().Set("X-Other", "no")
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "refreshed"})
			http.SetCookie(w, &http.Cookie{Name: "secret", Value: "x"})
			w.WriteHeader(http.StatusOK)
		case "":
			http.Redirect(w, r, "https://login.example.com/?rd="+r.Header.Get("X-Forwarded-Uri"), http.StatusFound)
		default:
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("denied"))
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func setup(t *testing.T, m *ForwardAuth) (http.Handler, *http.Header) {
	t.Helper()

	mw, err := m.Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	upstreamHeader := &http.Header{}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*upstreamHeader = r.Header.Clone()
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte("upstream"))
	})), upstreamHeader
}

func request(h http.Handler, method, target, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.7:1234"

	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestAllow(t *testing.T) {
	var got seen
	srv := authServer(t, &got)

	h, up := setup(t, &ForwardAuth{
		Address:                   srv.URL,
		AuthResponseHeaders:       []string{"X-User"},
		AuthResponseHeadersPrefix: "X-Auth-",
		AddAuthCookiesToResponse:  []string{"session"},
	})

	rec := request(h, http.MethodPost, "/app/x?y=1", "", "Authorization", "Bearer good", "X-User", "mallory", "X-Auth-Role", "forged")
	if rec.Code != http.StatusOK || rec.Body.String() != "upstream" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	if up.Get("X-User") != "alice" || up.Get("X-Auth-Groups") != "admin" || up.Get("X-Other") != "" {
		t.Fatalf("upstream headers %v", *up)
	}

	if up.Get("X-Auth-Role") != "" {
		t.Fatal("client header with prefix reached upstream")
	}

	if cookies := rec.Header().Values("Set-Cookie"); len(cookies) != 1 || !strings.HasPrefix(cookies[0], "session=refreshed") {
		t.Fatalf("cookies %v", cookies)
	}

	if got.method != http.MethodGet || got.header.Get("X-Forwarded-Method") != http.MethodPost ||
		got.header.Get("X-Forwarded-Uri") != "/app/x?y=1" || got.header.Get("X-Forwarded-For") != "203.0.113.7" {
		t.Fatalf("auth request %s %v", got.method, got.header)
	}
}

func TestDeny(t *testing.T) {
	var got seen
	srv := authServer(t, &got)
	h, _ := setup(t, &ForwardAuth{Address: srv.URL})

	rec := request(h, http.MethodGet, "/", "", "Authorization", "Bearer bad")
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != "denied" || rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}

	rec = request(h, http.MethodGet, "/private", "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://login.example.com/?rd=/private" {
		t.Fatalf("redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestForwardedHeaders(t *testing.T) {
	var got seen
	srv := authServer(t, &got)

	h, _ := setup(t, &ForwardAuth{Address: srv.URL})
	request(h, http.MethodGet, "/", "", "Authorization", "Bearer good", "X-Forwarded-Uri", "/forged", "X-Forwarded-For", "10.0.0.1")

	if got.header.Get("X-Forwarded-Uri") != "/" || got.header.Get("X-Forwarded-For") != "203.0.113.7" {
		t.Fatalf("untrusted headers kept %v", got.header)
	}

	h, _ = setup(t, &ForwardAuth{Address: srv.URL, TrustForwardHeader: true})
	request(h, http.MethodGet, "/", "", "Authorization", "Bearer good", "X-Forwarded-Uri", "/real", "X-Forwarded-For", "10.0.0.1")

	if got.header.Get("X-Forwarded-Uri") != "/real" || got.header.Get("X-Forwarded-For") != "10.0.0.1, 203.0.113.7" {
		t.Fatalf("trusted headers %v", got.header)
	}
}

func TestRequestHeadersAndBody(t *testing.T) {
	var got seen
	srv := authServer(t, &got)

	h, _ := setup(t, &ForwardAuth{
		Address:            srv.URL,
		Method:             "-",
		AuthRequestHeaders: []string{"Authorization"},
		ForwardBody:        true,
		MaxBodySize:        8,
	})

	rec := request(h, http.MethodPut, "/", "payload", "Authorization", "Bearer good", "Cookie", "a=b")
	if rec.Code != http.StatusOK || got.method != http.MethodPut || got.body != "payload" || got.header.Get("Cookie") != "" {
		t.Fatalf("status = %d method = %s body = %q header %v", rec.Code, got.method, got.body, got.header)
	}

	if rec := request(h, http.MethodPut, "/", "too large body", "Authorization", "Bearer good"); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large status = %d", rec.Code)
	}
}

func TestUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	h, _ := setup(t, &ForwardAuth{Address: srv.URL})
	if rec := request(h, http.MethodGet, "/", ""); rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSpoofedForwardingHeaders(t *testing.T) {
	var got seen
	srv := authServer(t, &got)

	h, _ := setup(t, &ForwardAuth{Address: srv.URL})
	request(h, http.MethodGet, "/", "",
		"Authorization", "Bearer good",
		"X-Forwarded-Proto", "https",
		"X-Real-Ip", "10.0.0.1",
		"Forwarded", "for=10.0.0.1",
		"X-Forwarded-Prefix", "/admin",
		"X-Original-Url", "https://app.example.com/admin",
		"X-Original-Method", "GET",
	)

	for _, k := range []string{"X-Real-Ip", "Forwarded", "X-Forwarded-Prefix", "X-Original-Url", "X-Original-Method"} {
		if v := got.header.Get(k); v != "" {
			t.Fatalf("client %s = %q reached the auth service", k, v)
		}
	}

	if got.header.Get("X-Forwarded-Proto") != "http" {
		t.Fatalf("proto = %q", got.header.Get("X-Forwarded-Proto"))
	}
}

func TestChunkedBody(t *testing.T) {
	var got seen
	srv := authServer(t, &got)

	h, _ := setup(t, &ForwardAuth{Address: srv.URL, ForwardBody: true, MaxBodySize: 8})

	for body, want := range map[string]int{"small": http.StatusOK, "much too large": http.StatusRequestEntityTooLarge} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.ContentLength = -1
		req.Header.Set("Authorization", "Bearer good")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != want {
			t.Fatalf("%q: status = %d", body, rec.Code)
		}
	}

	if got.body != "small" {
		t.Fatalf("auth body = %q", got.body)
	}
}

// countingServer answers ok and counts the connections opened to it.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	conns := &atomic.Int64{}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}

	srv.Start()
	t.Cleanup(srv.Close)

	return srv, conns
}

func TestAuthResponseClosedBeforeUpstream(t *testing.T) {
	srv, conns := countingServer(t)

	mw, err := (&ForwardAuth{Address: srv.URL}).Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	// the auth connection is reused only when its response was read and
	// closed before the upstream ran
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for range 5 {
		request(h, http.MethodGet, "/", "")
	}

	if n := conns.Load(); n != 1 {
		t.Fatalf("auth connections = %d, want 1", n)
	}
}

func TestAuthBodyReleasedDuringUpstream(t *testing.T) {
	srv, conns := countingServer(t)

	mw, err := (&ForwardAuth{Address: srv.URL}).Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	// a long running upstream (websocket, SSE) checks again while the outer
	// request still runs; with the first auth response still open a second
	// connection would be needed
	inner := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	outer := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		request(inner, http.MethodGet, "/", "")
	}))

	request(outer, http.MethodGet, "/", "")

	if n := conns.Load(); n != 1 {
		t.Fatalf("auth connections = %d, want 1", n)
	}
}

func TestCookieWriter(t *testing.T) {
	var got seen
	srv := authServer(t, &got)

	mw, err := (&ForwardAuth{Address: srv.URL, AddAuthCookiesToResponse: []string{"session"}}).Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var hijacker bool
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, hijacker = w.(http.Hijacker)
	}))

	rec := request(h, http.MethodGet, "/", "", "Authorization", "Bearer good")

	if !hijacker {
		t.Fatal("writer is not a http.Hijacker")
	}

	if !strings.HasPrefix(rec.Header().Get("Set-Cookie"), "session=refreshed") {
		t.Fatalf("cookie missing when upstream writes nothing: %v", rec.Header())
	}
}

func TestInvalidAddress(t *testing.T) {
	for _, a := range []string{"", "auth:9091", "ftp://x"} {
		if _, err := (&ForwardAuth{Address: a}).Middleware(t.Context()); err == nil {
			t.Fatalf("%q accepted", a)
		}
	}
}
