package antibot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func newAntibot(t *testing.T, m *Antibot) (*antibot, http.Handler) {
	t.Helper()

	if m.Secret == "" {
		m.Secret = testSecret
	}

	a, err := m.build()
	if err != nil {
		t.Fatal(err)
	}

	return a, a.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

func request(method, target, remote string, headers map[string]string, body url.Values) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(body.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}

	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

var htmlAccept = map[string]string{"Accept": "text/html,application/xhtml+xml"}

func field(t *testing.T, body, name string) string {
	t.Helper()

	m := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("field %s not found in page", name)
	}

	return html.UnescapeString(m[1])
}

func solve(challenge string, difficulty int) string {
	for nonce := 0; ; nonce++ {
		n := strconv.Itoa(nonce)
		if leadingZeroBits(sha256.Sum256([]byte(challenge+n))) >= difficulty {
			return n
		}
	}
}

func TestJavascriptFlow(t *testing.T) {
	_, h := newAntibot(t, &Antibot{Difficulty: 8})

	rec := serve(h, request(http.MethodGet, "/app?x=1", "1.2.3.4:1000", htmlAccept, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("challenge status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q", ct)
	}

	body := rec.Body.String()
	challenge := field(t, body, "challenge")
	if got := field(t, body, "redirect"); got != "/app?x=1" {
		t.Fatalf("redirect = %q", got)
	}
	if !strings.Contains(body, "crypto.subtle.digest") {
		t.Fatal("page has no proof of work script")
	}

	// wrong nonce
	var bad string
	for n := 0; ; n++ {
		if leadingZeroBits(sha256.Sum256([]byte(challenge+strconv.Itoa(n)))) < 8 {
			bad = strconv.Itoa(n)

			break
		}
	}

	rec = serve(h, request(http.MethodPost, defaultPath, "1.2.3.4:1000", nil,
		url.Values{"challenge": {challenge}, "nonce": {bad}, "redirect": {"/app?x=1"}}))
	if rec.Code != http.StatusForbidden || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("bad nonce status = %d", rec.Code)
	}

	// solved from another IP
	nonce := solve(challenge, 8)
	rec = serve(h, request(http.MethodPost, defaultPath, "5.6.7.8:1000", nil,
		url.Values{"challenge": {challenge}, "nonce": {nonce}, "redirect": {"/app?x=1"}}))
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("challenge accepted from another ip")
	}

	rec = serve(h, request(http.MethodPost, defaultPath, "1.2.3.4:1000", nil,
		url.Values{"challenge": {challenge}, "nonce": {nonce}, "redirect": {"/app?x=1"}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("solve status = %d, body %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/app?x=1" {
		t.Fatalf("location = %q", loc)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "turna_antibot" || !cookies[0].HttpOnly || !cookies[0].Secure ||
		cookies[0].MaxAge != int((24*time.Hour)/time.Second) {
		t.Fatalf("cookies = %+v", cookies)
	}

	req := request(http.MethodGet, "/app", "1.2.3.4:1000", htmlAccept, nil)
	req.AddCookie(cookies[0])
	if rec := serve(h, req); rec.Code != http.StatusNoContent {
		t.Fatalf("with cookie status = %d", rec.Code)
	}

	// the cookie is bound to the IP
	req = request(http.MethodGet, "/app", "5.6.7.8:1000", nil, nil)
	req.AddCookie(cookies[0])
	if rec := serve(h, req); rec.Code != http.StatusForbidden {
		t.Fatalf("cookie from other ip status = %d", rec.Code)
	}
}

func TestPoW(t *testing.T) {
	a, _ := newAntibot(t, &Antibot{Difficulty: 10})

	now := time.Now()
	a.now = func() time.Time { return now }

	challenge := a.newChallenge("1.2.3.4")
	nonce := solve(challenge, 10)

	if err := a.verifyPoW(challenge, nonce, "1.2.3.4"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		challenge string
		nonce     string
		ip        string
	}{
		{name: "tampered challenge", challenge: "1" + challenge, nonce: nonce, ip: "1.2.3.4"},
		{name: "malformed challenge", challenge: "abc", nonce: nonce, ip: "1.2.3.4"},
		{name: "other ip", challenge: challenge, nonce: nonce, ip: "1.2.3.5"},
		{name: "empty nonce", challenge: challenge, nonce: "", ip: "1.2.3.4"},
		{name: "non numeric nonce", challenge: challenge, nonce: nonce + "x", ip: "1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := a.verifyPoW(tt.challenge, tt.nonce, tt.ip); err == nil {
				t.Fatal("expected error")
			}
		})
	}

	t.Run("expired", func(t *testing.T) {
		a.now = func() time.Time { return now.Add(6 * time.Minute) }
		defer func() { a.now = func() time.Time { return now } }()

		if err := a.verifyPoW(challenge, nonce, "1.2.3.4"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("higher difficulty", func(t *testing.T) {
		b, _ := newAntibot(t, &Antibot{Difficulty: 20})
		// a challenge signed for one difficulty is not valid for another
		if err := b.verifyPoW(challenge, nonce, "1.2.3.4"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestCookie(t *testing.T) {
	a, _ := newAntibot(t, &Antibot{})

	now := time.Now()
	a.now = func() time.Time { return now }

	value := a.cookieValue("1.2.3.4", now.Add(time.Hour))
	if !a.validCookie(value, "1.2.3.4") {
		t.Fatal("valid cookie rejected")
	}

	exp, sig, _ := strings.Cut(value, ".")
	later := strconv.FormatInt(now.Add(2*time.Hour).Unix(), 10)

	tests := []struct {
		name  string
		value string
		ip    string
	}{
		{name: "other ip", value: value, ip: "1.2.3.5"},
		{name: "tampered expiry", value: later + "." + sig, ip: "1.2.3.4"},
		{name: "tampered signature", value: exp + "." + sig[:len(sig)-2] + "AA", ip: "1.2.3.4"},
		{name: "no signature", value: exp, ip: "1.2.3.4"},
		{name: "garbage", value: "x.y", ip: "1.2.3.4"},
		{name: "too long", value: a.cookieValue("1.2.3.4", now.Add(48*time.Hour)), ip: "1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if a.validCookie(tt.value, tt.ip) {
				t.Fatal("invalid cookie accepted")
			}
		})
	}

	t.Run("expired", func(t *testing.T) {
		a.now = func() time.Time { return now.Add(2 * time.Hour) }
		defer func() { a.now = func() time.Time { return now } }()

		if a.validCookie(value, "1.2.3.4") {
			t.Fatal("expired cookie accepted")
		}
	})

	t.Run("other secret", func(t *testing.T) {
		b, _ := newAntibot(t, &Antibot{Secret: "another-secret-of-32-bytes-long!"})
		if b.validCookie(value, "1.2.3.4") {
			t.Fatal("cookie of another secret accepted")
		}
	})

	t.Run("unbound", func(t *testing.T) {
		bind := false
		b, _ := newAntibot(t, &Antibot{BindIP: &bind})
		if !b.validCookie(b.cookieValue("1.2.3.4", time.Now().Add(time.Hour)), "9.9.9.9") {
			t.Fatal("unbound cookie rejected from another ip")
		}
	})
}

func TestCaptcha(t *testing.T) {
	var got url.Values
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm

		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("response") == "good" && r.PostForm.Get("secret") == "server-secret" {
			_, _ = w.Write([]byte(`{"success":true}`))

			return
		}

		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	defer provider.Close()

	for typ, p := range providers {
		t.Run(typ, func(t *testing.T) {
			_, h := newAntibot(t, &Antibot{
				Type:      typ,
				SiteKey:   "site-key",
				SecretKey: "server-secret",
				VerifyURL: provider.URL,
			})

			rec := serve(h, request(http.MethodGet, "/", "1.2.3.4:1000", htmlAccept, nil))
			body := rec.Body.String()
			if !strings.Contains(body, p.script) || !strings.Contains(body, `class="`+p.widget+`"`) ||
				!strings.Contains(body, `data-sitekey="site-key"`) {
				t.Fatalf("page misses widget: %s", body)
			}

			rec = serve(h, request(http.MethodPost, defaultPath, "1.2.3.4:1000", nil,
				url.Values{p.field: {"bad"}, "redirect": {"/x"}}))
			if rec.Code != http.StatusForbidden || len(rec.Result().Cookies()) != 0 {
				t.Fatalf("bad token status = %d", rec.Code)
			}

			rec = serve(h, request(http.MethodPost, defaultPath, "1.2.3.4:1000", nil,
				url.Values{p.field: {"good"}, "redirect": {"/x"}}))
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/x" || len(rec.Result().Cookies()) != 1 {
				t.Fatalf("good token status = %d", rec.Code)
			}
			if got.Get("remoteip") != "1.2.3.4" {
				t.Fatalf("remoteip = %q", got.Get("remoteip"))
			}
		})
	}

	t.Run("provider down", func(t *testing.T) {
		down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer down.Close()

		a, _ := newAntibot(t, &Antibot{Type: "turnstile", SiteKey: "k", SecretKey: "s", VerifyURL: down.URL})
		if err := a.verifyCaptcha(context.Background(), "good", "1.2.3.4"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSafeRedirect(t *testing.T) {
	tests := map[string]string{
		"/app?x=1":              "/app?x=1",
		"/":                     "/",
		"":                      "/",
		"//evil.com":            "/",
		"/\\evil.com":           "/",
		"https://evil.com/":     "/",
		"evil.com":              "/",
		"javascript:alert(1)":   "/",
		"/ok\r\nSet-Cookie: x":  "/",
		"/\tevil":               "/",
		"/path/with//double":    "/path/with//double",
		"/%2F%2Fevil.com":       "/%2F%2Fevil.com",
		"/a#frag":               "/a#frag",
		"http:/evil.com":        "/",
		"/\\/evil.com":          "/",
		"/x?next=//evil.com":    "/x?next=//evil.com",
		"\\\\evil.com":          "/",
		" /leading-space":       "/",
		"/ok?a=b&c=d":           "/ok?a=b&c=d",
		"/@evil.com":            "/@evil.com",
		"/../../etc":            "/../../etc",
		"/%0d%0aSet-Cookie:%20": "/%0d%0aSet-Cookie:%20",
	}

	for in, want := range tests {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}

	_, h := newAntibot(t, &Antibot{Difficulty: 4})

	rec := serve(h, request(http.MethodGet, "/", "1.2.3.4:1000", htmlAccept, nil))
	challenge := field(t, rec.Body.String(), "challenge")

	rec = serve(h, request(http.MethodPost, defaultPath, "1.2.3.4:1000", nil,
		url.Values{"challenge": {challenge}, "nonce": {solve(challenge, 4)}, "redirect": {"//evil.com/x"}}))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}

	// the challenge endpoint page keeps the redirect only when it is safe
	rec = serve(h, request(http.MethodGet, defaultPath+"?redirect=https://evil.com", "1.2.3.4:1000", htmlAccept, nil))
	if got := field(t, rec.Body.String(), "redirect"); got != "/" {
		t.Fatalf("redirect = %q", got)
	}
}

func TestBypass(t *testing.T) {
	_, h := newAntibot(t, &Antibot{
		SkipPaths:      []string{"/health", "/static/"},
		Allow:          []string{"10.0.0.0/8", "192.168.1.1"},
		TrustedProxies: []string{"172.16.0.0/12"},
	})

	tests := []struct {
		name    string
		target  string
		remote  string
		headers map[string]string
		want    int
	}{
		{name: "skip path", target: "/health", remote: "1.2.3.4:1", want: http.StatusNoContent},
		{name: "skip prefix", target: "/static/app.js", remote: "1.2.3.4:1", want: http.StatusNoContent},
		{name: "allow cidr", target: "/", remote: "10.1.2.3:1", want: http.StatusNoContent},
		{name: "allow ip", target: "/", remote: "192.168.1.1:1", want: http.StatusNoContent},
		{name: "allow through trusted proxy", target: "/", remote: "172.16.0.1:1",
			headers: map[string]string{"X-Forwarded-For": "10.0.0.5"}, want: http.StatusNoContent},
		{name: "forged header ignored", target: "/", remote: "1.2.3.4:1",
			headers: map[string]string{"X-Forwarded-For": "10.0.0.5"}, want: http.StatusForbidden},
		{name: "api request without cookie", target: "/api", remote: "1.2.3.4:1", want: http.StatusForbidden},
		{name: "post without cookie", target: "/", remote: "1.2.3.4:1", headers: htmlAccept, want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := http.MethodGet
			if strings.HasPrefix(tt.name, "post") {
				method = http.MethodPost
			}

			rec := serve(h, request(method, tt.target, tt.remote, tt.headers, nil))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if rec.Code == http.StatusForbidden && !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestConfig(t *testing.T) {
	tests := []struct {
		name string
		m    Antibot
	}{
		{name: "unknown type", m: Antibot{Type: "puzzle"}},
		{name: "captcha without keys", m: Antibot{Type: "turnstile"}},
		{name: "bad verify url", m: Antibot{Type: "hcaptcha", SiteKey: "a", SecretKey: "b", VerifyURL: "ftp://x"}},
		{name: "short secret", m: Antibot{Secret: "short"}},
		{name: "difficulty", m: Antibot{Difficulty: 40}},
		{name: "relative path", m: Antibot{Path: "antibot"}},
		{name: "same site none insecure", m: Antibot{Cookie: Cookie{SameSite: "none", Secure: new(bool)}}},
		{name: "bad same site", m: Antibot{Cookie: Cookie{SameSite: "sometimes"}}},
		{name: "bad allow", m: Antibot{Allow: []string{"nope"}}},
		{name: "bad proxy", m: Antibot{TrustedProxies: []string{"nope"}}},
		{name: "bad status", m: Antibot{StatusCode: 42}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.m.Secret == "" {
				tt.m.Secret = testSecret
			}

			if _, err := tt.m.Middleware(context.Background()); err == nil {
				t.Fatal("expected error")
			}
		})
	}

	t.Run("random secret", func(t *testing.T) {
		m := &Antibot{}
		if _, err := m.Middleware(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSkipPathsNormalized(t *testing.T) {
	_, h := newAntibot(t, &Antibot{SkipPaths: []string{"/health", "/static/"}})

	for target, want := range map[string]int{
		"/static/app.js":      http.StatusNoContent,
		"/static/../admin":    http.StatusForbidden,
		"/health/../../admin": http.StatusForbidden,
		"/static/a/../b.js":   http.StatusNoContent,
	} {
		if rec := serve(h, request(http.MethodGet, target, "1.2.3.4:1", nil, nil)); rec.Code != want {
			t.Errorf("%s: status = %d, want %d", target, rec.Code, want)
		}
	}
}

func TestConfigUnchanged(t *testing.T) {
	m := &Antibot{Secret: testSecret, Type: " JavaScript ", SkipPaths: []string{"/health"}}
	before, _ := json.Marshal(m)

	if _, err := m.Middleware(context.Background()); err != nil {
		t.Fatal(err)
	}

	if after, _ := json.Marshal(m); string(after) != string(before) {
		t.Fatalf("config changed by Middleware: %s -> %s", before, after)
	}
}
