package ipfilter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, m *IPFilter, remote string, headers map[string]string) int {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Code
}

func TestIPFilter(t *testing.T) {
	tests := []struct {
		name    string
		filter  IPFilter
		remote  string
		headers map[string]string
		want    int
	}{
		{"allowed", IPFilter{Allow: []string{"10.0.0.0/8"}}, "10.1.2.3:1234", nil, http.StatusNoContent},
		{"not allowed", IPFilter{Allow: []string{"10.0.0.0/8"}}, "192.0.2.1:1234", nil, http.StatusForbidden},
		{"single ip", IPFilter{Allow: []string{"192.0.2.1"}}, "192.0.2.1:1234", nil, http.StatusNoContent},
		{"ipv6", IPFilter{Allow: []string{"2001:db8::/32"}}, "[2001:db8::1]:1234", nil, http.StatusNoContent},
		{"denied", IPFilter{Deny: []string{"192.0.2.0/24"}}, "192.0.2.1:1234", nil, http.StatusForbidden},
		{"not denied", IPFilter{Deny: []string{"192.0.2.0/24"}}, "198.51.100.1:1234", nil, http.StatusNoContent},
		{"deny wins", IPFilter{Allow: []string{"10.0.0.0/8"}, Deny: []string{"10.0.0.5"}}, "10.0.0.5:1", nil, http.StatusForbidden},
		{"custom status", IPFilter{Allow: []string{"10.0.0.0/8"}, StatusCode: http.StatusNotFound}, "192.0.2.1:1", nil, http.StatusNotFound},
		{
			"forwarded header ignored without trusted proxies",
			IPFilter{Allow: []string{"10.0.0.0/8"}},
			"192.0.2.1:1234",
			map[string]string{"X-Forwarded-For": "10.0.0.1"},
			http.StatusForbidden,
		},
		{
			"forwarded header from trusted proxy",
			IPFilter{Allow: []string{"203.0.113.0/24"}, TrustedProxies: []string{"10.0.0.0/8"}},
			"10.0.0.1:1234",
			map[string]string{"X-Forwarded-For": "203.0.113.7"},
			http.StatusNoContent,
		},
		{
			"proxy address is not the client",
			IPFilter{Allow: []string{"10.0.0.0/8"}, TrustedProxies: []string{"10.0.0.0/8"}},
			"10.0.0.1:1234",
			map[string]string{"X-Forwarded-For": "203.0.113.7"},
			http.StatusForbidden,
		},
		{
			"malformed header from trusted proxy",
			IPFilter{Allow: []string{"10.0.0.0/8"}, TrustedProxies: []string{"10.0.0.0/8"}},
			"10.0.0.1:1234",
			map[string]string{"X-Forwarded-For": "not-an-ip"},
			http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serve(t, &tt.filter, tt.remote, tt.headers); got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIPFilterConfig(t *testing.T) {
	for _, m := range []IPFilter{
		{},
		{Allow: []string{"not-a-cidr"}},
		{Allow: []string{"10.0.0.0/8"}, TrustedProxies: []string{"bad"}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
