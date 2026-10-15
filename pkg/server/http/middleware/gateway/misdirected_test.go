package gateway

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Cases of the HTTPRouteHTTPSListenerDetectMisdirectedRequests conformance
// test.
func TestMisdirected(t *testing.T) {
	g := &Gateway{Listeners: []string{"", "second-example.org", "*.wildcard.org", "fourth-example.wildcard.org"}}

	for _, tt := range []struct {
		sni, host string
		want      bool
	}{
		{"example.org", "example.org", false},
		{"example.org", "second-example.org", true},
		{"example.org", "unknown-example.org", false},
		{"second-example.org", "second-example.org", false},
		{"second-example.org", "example.org", true},
		{"second-example.org", "unknown-example.org", true},
		{"third-example.wildcard.org", "third-example.wildcard.org", false},
		{"third-example.wildcard.org", "fith-example.wildcard.org", false},
		{"third-example.wildcard.org", "fourth-example.wildcard.org", true},
		{"third-example.wildcard.org", "second-example.org", true},
		{"fourth-example.wildcard.org", "fourth-example.wildcard.org", false},
		{"fourth-example.wildcard.org", "fith-example.wildcard.org", true},
		{"unknown-example.org", "example.org", false},
		{"Second-Example.org.", "second-example.org:443", false},
		{"", "second-example.org", false},
	} {
		r := httptest.NewRequest(http.MethodGet, "https://"+tt.host+"/", nil)
		r.TLS = &tls.ConnectionState{ServerName: tt.sni}

		if got := g.misdirected(r); got != tt.want {
			t.Errorf("sni %q host %q: misdirected = %v, want %v", tt.sni, tt.host, got, tt.want)
		}
	}

	// plain text and single listeners are never misdirected
	plain := httptest.NewRequest(http.MethodGet, "http://second-example.org/", nil)
	if g.misdirected(plain) {
		t.Error("plain text request misdirected")
	}

	single := &Gateway{Listeners: []string{"a.example.com"}}
	r := httptest.NewRequest(http.MethodGet, "https://b.example.com/", nil)
	r.TLS = &tls.ConnectionState{ServerName: "a.example.com"}

	if single.misdirected(r) {
		t.Error("single listener misdirected")
	}
}

func TestMisdirectedAnswers421(t *testing.T) {
	g := &Gateway{Listeners: []string{"", "second-example.org"}}

	r := httptest.NewRequest(http.MethodGet, "https://second-example.org/", nil)
	r.TLS = &tls.ConnectionState{ServerName: "example.org"}

	if rec := serve(t, g, r); rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}
