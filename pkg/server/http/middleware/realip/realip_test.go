package realip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type seen struct {
	remote, realIP, xff string
}

func serve(t *testing.T, m *RealIP, remote string, headers map[string]string) (int, seen) {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	var got seen
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{r.RemoteAddr, r.Header.Get("X-Real-IP"), r.Header.Get("X-Forwarded-For")}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Code, got
}

func TestRealIP(t *testing.T) {
	m := &RealIP{TrustedProxies: []string{"10.0.0.0/8"}}

	tests := []struct {
		name    string
		remote  string
		headers map[string]string
		want    seen
	}{
		{
			name:    "untrusted peer headers dropped",
			remote:  "203.0.113.5:1234",
			headers: map[string]string{"X-Forwarded-For": "1.1.1.1", "X-Real-IP": "1.1.1.1"},
			want:    seen{"203.0.113.5:1234", "203.0.113.5", ""},
		},
		{
			name:    "trusted proxy",
			remote:  "10.0.0.2:1234",
			headers: map[string]string{"X-Forwarded-For": "198.51.100.7"},
			want:    seen{"198.51.100.7:1234", "198.51.100.7", ""},
		},
		{
			name:    "spoofed left hops kept",
			remote:  "10.0.0.2:1234",
			headers: map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.7, 10.0.0.9"},
			want:    seen{"198.51.100.7:1234", "198.51.100.7", "6.6.6.6"},
		},
		{
			name:    "x-real-ip fallback",
			remote:  "10.0.0.2:1234",
			headers: map[string]string{"X-Real-IP": "198.51.100.7"},
			want:    seen{"198.51.100.7:1234", "198.51.100.7", ""},
		},
		{
			name:   "no header",
			remote: "10.0.0.2:1234",
			want:   seen{"10.0.0.2:1234", "10.0.0.2", ""},
		},
		{
			name:    "ipv6",
			remote:  "[2001:db8::1]:1234",
			headers: map[string]string{"X-Forwarded-For": "1.1.1.1"},
			want:    seen{"[2001:db8::1]:1234", "2001:db8::1", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got := serve(t, m, tt.remote, tt.headers)
			if code != http.StatusOK {
				t.Fatalf("status = %d", code)
			}

			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCustomHeader(t *testing.T) {
	m := &RealIP{TrustedProxies: []string{"10.0.0.0/8"}, Header: "CF-Connecting-IP"}

	_, got := serve(t, m, "10.0.0.2:1", map[string]string{
		"CF-Connecting-IP": "198.51.100.7",
		"X-Forwarded-For":  "6.6.6.6",
	})

	if got.remote != "198.51.100.7:1" {
		t.Fatalf("remote = %q", got.remote)
	}

	// without the header the proxy is the client, X-Forwarded-For is ignored
	_, got = serve(t, m, "10.0.0.2:1", map[string]string{"X-Forwarded-For": "6.6.6.6"})
	if got.remote != "10.0.0.2:1" {
		t.Fatalf("remote = %q", got.remote)
	}
}

func TestMalformed(t *testing.T) {
	m := &RealIP{TrustedProxies: []string{"10.0.0.0/8"}}

	code, _ := serve(t, m, "10.0.0.2:1", map[string]string{"X-Forwarded-For": "nope"})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*RealIP{{}, {TrustedProxies: []string{"bad"}}} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
