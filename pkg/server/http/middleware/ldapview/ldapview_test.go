package ldapview

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
)

type fakeLDAP struct {
	addr  string
	mu    sync.Mutex
	binds [][2]string
}

// newFakeLDAP answers binds (cn=bad fails) and returns two fixed entries for
// any search.
func newFakeLDAP(t *testing.T) *fakeLDAP {
	t.Helper()

	return newFakeLDAPTLS(t, nil)
}

// newFakeLDAPTLS serves ldaps:// when cfg is set.
func newFakeLDAPTLS(t *testing.T, cfg *tls.Config) *fakeLDAP {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	scheme := "ldap://"
	if cfg != nil {
		l = tls.NewListener(l, cfg)
		scheme = "ldaps://"
	}

	t.Cleanup(func() { _ = l.Close() })

	f := &fakeLDAP{addr: scheme + l.Addr().String()}

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go f.serve(c)
		}
	}()

	return f
}

func envelope(msgID any, op *ber.Packet) []byte {
	p := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
	p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, msgID, ""))
	p.AppendChild(op)

	return p.Bytes()
}

func result(app ber.Tag, code int64) *ber.Packet {
	r := ber.Encode(ber.ClassApplication, ber.TypeConstructed, app, nil, "")
	r.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, code, ""))
	r.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
	r.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))

	return r
}

func searchEntry(dn string, attrs map[string][]string) *ber.Packet {
	e := ber.Encode(ber.ClassApplication, ber.TypeConstructed, ldap.ApplicationSearchResultEntry, nil, "")
	e.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, dn, ""))

	list := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
	for name, values := range attrs {
		a := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
		a.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, name, ""))

		set := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
		for _, v := range values {
			set.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, v, ""))
		}

		a.AppendChild(set)
		list.AppendChild(a)
	}

	e.AppendChild(list)

	return e
}

func (f *fakeLDAP) serve(c net.Conn) {
	defer c.Close()

	for {
		p, err := ber.ReadPacket(c)
		if err != nil {
			return
		}

		msgID := p.Children[0].Value
		op := p.Children[1]

		switch op.Tag {
		case ldap.ApplicationBindRequest:
			dn := op.Children[1].Value.(string)
			pass := op.Children[2].Data.String()

			f.mu.Lock()
			f.binds = append(f.binds, [2]string{dn, pass})
			f.mu.Unlock()

			code := int64(ldap.LDAPResultSuccess)
			if dn == "cn=bad" {
				code = ldap.LDAPResultInvalidCredentials
			}

			_, _ = c.Write(envelope(msgID, result(ldap.ApplicationBindResponse, code)))
		case ldap.ApplicationSearchRequest:
			_, _ = c.Write(envelope(msgID, searchEntry("uid=alice,ou=people,dc=example,dc=org", map[string][]string{
				"uid":       {"alice"},
				"cn":        {"Alice", "A. Liddell"},
				"jpegPhoto": {"\x00\x01\xff"},
			})))
			_, _ = c.Write(envelope(msgID, searchEntry("uid=bob,ou=people,dc=example,dc=org", map[string][]string{"uid": {"bob"}})))
			_, _ = c.Write(envelope(msgID, result(ldap.ApplicationSearchResultDone, ldap.LDAPResultSuccess)))
		case ldap.ApplicationExtendedRequest:
			_, _ = c.Write(envelope(msgID, result(ldap.ApplicationExtendedResponse, ldap.LDAPResultSuccess)))
		case ldap.ApplicationUnbindRequest:
			return
		}
	}
}

func newView(t *testing.T, srv *fakeLDAP) http.Handler {
	t.Helper()

	m := &LdapView{
		PrefixPath: "/ldap",
		Servers: []Server{
			{Name: "test", Addr: srv.addr, UserDNTemplate: "uid=%s,ou=people,dc=example,dc=org"},
		},
		DisableBindModes: []string{},
	}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(nil)
}

func do(t *testing.T, h http.Handler, method, path string, body any, cookies []*http.Cookie, headers ...string) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}

	r := httptest.NewRequest(method, path, &buf)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}

	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}

	for _, c := range cookies {
		r.AddCookie(c)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	return w
}

func TestLoginModes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		req      loginRequest
		status   int
		wantBind [2]string
	}{
		{name: "simple with template", req: loginRequest{Server: "test", Mode: BindSimple, Username: "alice", Password: "pw"}, status: 200, wantBind: [2]string{"uid=alice,ou=people,dc=example,dc=org", "pw"}},
		{name: "simple full dn", req: loginRequest{Server: "test", Mode: BindSimple, Username: "cn=admin,dc=x", Password: "pw"}, status: 200, wantBind: [2]string{"cn=admin,dc=x", "pw"}},
		{name: "anonymous drops credentials", req: loginRequest{Server: "test", Mode: BindAnonymous, Username: "cn=admin", Password: "pw"}, status: 200},
		{name: "unauthenticated drops password", req: loginRequest{Server: "test", Mode: BindUnauthenticated, Username: "cn=admin,dc=x", Password: "pw"}, status: 200, wantBind: [2]string{"cn=admin,dc=x", ""}},
		{name: "bad credentials", req: loginRequest{Server: "test", Mode: BindSimple, Username: "cn=bad", Password: "pw"}, status: 401, wantBind: [2]string{"cn=bad", "pw"}},
		{name: "simple without password", req: loginRequest{Server: "test", Mode: BindSimple, Username: "alice"}, status: 401},
		{name: "custom server unreachable", req: loginRequest{Addr: "ldap://127.0.0.1:1", Mode: BindAnonymous}, status: 401},
		{name: "custom server bad scheme", req: loginRequest{Addr: "http://127.0.0.1:1", Mode: BindAnonymous}, status: 401},
		{name: "unknown mode", req: loginRequest{Server: "test", Mode: "sasl"}, status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := newFakeLDAP(t)
			h := newView(t, srv)

			w := do(t, h, http.MethodPost, "/ldap/api/login", tc.req, nil)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body)
			}

			srv.mu.Lock()
			defer srv.mu.Unlock()

			if tc.status == 200 || tc.wantBind != [2]string{} {
				if len(srv.binds) != 1 || srv.binds[0] != tc.wantBind {
					t.Fatalf("binds %q, want %q", srv.binds, tc.wantBind)
				}
			}
		})
	}
}

func TestSessionSearch(t *testing.T) {
	t.Parallel()

	srv := newFakeLDAP(t)
	h := newView(t, srv)

	if w := do(t, h, http.MethodPost, "/ldap/api/search", searchRequest{}, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("search without session: %d", w.Code)
	}

	w := do(t, h, http.MethodPost, "/ldap/api/login", loginRequest{Server: "test", Mode: BindAnonymous}, nil)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/ldap/" {
		t.Fatalf("cookie %+v", cookies)
	}

	w = do(t, h, http.MethodGet, "/ldap/api/info", nil, cookies)
	if !strings.Contains(w.Body.String(), `"mode":"anonymous"`) {
		t.Fatalf("info without session: %s", w.Body)
	}

	w = do(t, h, http.MethodPost, "/ldap/api/search", searchRequest{BaseDN: "dc=example,dc=org", Filter: "(uid=*)"}, cookies)
	if w.Code != 200 {
		t.Fatalf("search: %d %s", w.Code, w.Body)
	}

	var res searchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}

	if len(res.Entries) != 2 || res.Entries[0].DN != "uid=alice,ou=people,dc=example,dc=org" {
		t.Fatalf("entries %+v", res.Entries)
	}

	for _, a := range res.Entries[0].Attributes {
		if a.Name == "jpegPhoto" && (!a.Binary || a.Values[0] != "AAH/") {
			t.Fatalf("binary attribute %+v", a)
		}
	}

	if w := do(t, h, http.MethodPost, "/ldap/api/search", searchRequest{Filter: "(uid=alice"}, cookies); w.Code != http.StatusBadRequest {
		t.Fatalf("bad filter: %d", w.Code)
	}

	if w := do(t, h, http.MethodPost, "/ldap/api/search", searchRequest{}, cookies, "Sec-Fetch-Site", "cross-site"); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d", w.Code)
	}

	if w := do(t, h, http.MethodPost, "/ldap/api/logout", struct{}{}, cookies); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}

	if w := do(t, h, http.MethodPost, "/ldap/api/search", searchRequest{}, cookies); w.Code != http.StatusUnauthorized {
		t.Fatalf("search after logout: %d", w.Code)
	}
}

func TestMiddlewareConfig(t *testing.T) {
	t.Parallel()

	for _, m := range []*LdapView{
		{Servers: []Server{{Addr: "http://x"}}},
		{Servers: []Server{{Addr: "ldap://x", UserDNTemplate: "uid=alice,dc=x"}}},
		{Servers: []Server{{Addr: "ldap://x", UserDNTemplate: "uid=%s,o=%s"}}},
		{DisableBindModes: []string{"kerberos"}},
		{DisableBindModes: []string{BindSimple, BindAnonymous, BindUnauthenticated}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Errorf("want error for %+v", m)
		}
	}
}

func TestUserDN(t *testing.T) {
	t.Parallel()

	tpl := "uid=%s,dc=x"
	for in, want := range map[string]string{
		"alice":         "uid=alice,dc=x",
		"a,b":           "uid=a\\,b,dc=x",
		"cn=admin":      "cn=admin",
		"a@example.com": "a@example.com",
		"DOMAIN\\user":  "DOMAIN\\user",
	} {
		if got := userDN(tpl, in); got != want {
			t.Errorf("userDN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchLimits(t *testing.T) {
	t.Parallel()

	srv := newFakeLDAP(t)
	m := &LdapView{Servers: []Server{{Name: "test", Addr: srv.addr}}, MaxResponseSize: 260}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := mw(nil)

	w := do(t, h, http.MethodPost, "/api/login", loginRequest{Addr: srv.addr, Mode: BindAnonymous}, nil)
	if w.Code != 200 {
		t.Fatalf("custom login: %d %s", w.Code, w.Body)
	}

	cookies := w.Result().Cookies()

	var res searchResponse

	w = do(t, h, http.MethodPost, "/api/search", searchRequest{MaxValues: 1}, cookies)
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}

	if len(res.Entries) != 1 || !res.Truncated || !strings.Contains(res.Warning, "response size") {
		t.Fatalf("want one entry cut by response size, got %+v", res)
	}

	for _, a := range res.Entries[0].Attributes {
		if len(a.Values) > 1 {
			t.Fatalf("max_values ignored: %+v", a)
		}
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	srv := newFakeLDAP(t)
	h := newView(t, srv)

	w := do(t, h, http.MethodPost, "/ldap/api/login", loginRequest{Server: "test", Mode: BindAnonymous}, nil)
	cookies := w.Result().Cookies()

	for _, tc := range []struct {
		name   string
		req    verifyRequest
		ok     bool
		bindDN string
		status int
	}{
		{name: "template", req: verifyRequest{Username: "carol", Password: "pw"}, ok: true, bindDN: "uid=carol,ou=people,dc=example,dc=org"},
		{name: "wrong password", req: verifyRequest{Username: "cn=bad", Password: "pw"}, bindDN: "cn=bad"},
		{name: "ambiguous search", req: verifyRequest{Username: "x", Password: "pw", Filter: "(uid=%s)"}},
		{name: "filter without placeholder", req: verifyRequest{Username: "x", Password: "pw", Filter: "(uid=x)"}},
		{name: "empty password", req: verifyRequest{Username: "x"}, status: http.StatusBadRequest},
	} {
		w := do(t, h, http.MethodPost, "/ldap/api/verify", tc.req, cookies)

		want := tc.status
		if want == 0 {
			want = http.StatusOK
		}

		if w.Code != want {
			t.Fatalf("%s: status %d: %s", tc.name, w.Code, w.Body)
		}

		if want != http.StatusOK {
			continue
		}

		var res verifyResponse
		_ = json.Unmarshal(w.Body.Bytes(), &res)

		if res.OK != tc.ok || res.BindDN != tc.bindDN {
			t.Fatalf("%s: got %+v", tc.name, res)
		}
	}

	// the session's own bind stays anonymous
	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.binds[0] != [2]string{} {
		t.Fatalf("first bind %q", srv.binds[0])
	}
}

func TestMaxSessions(t *testing.T) {
	t.Parallel()

	srv := newFakeLDAP(t)
	m := &LdapView{Servers: []Server{{Name: "test", Addr: srv.addr}}, MaxSessions: 2}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := mw(nil)

	var first []*http.Cookie
	for i := range 3 {
		w := do(t, h, http.MethodPost, "/api/login", loginRequest{Server: "test", Mode: BindAnonymous}, nil)
		if i == 0 {
			first = w.Result().Cookies()
		}
	}

	if n := len(m.sessions.sessions); n != 2 {
		t.Fatalf("%d sessions, want 2", n)
	}

	if w := do(t, h, http.MethodPost, "/api/search", searchRequest{}, first); w.Code != http.StatusUnauthorized {
		t.Fatalf("oldest session still works: %d", w.Code)
	}
}
