package config

import (
	"net/http"
	"net/url"
	"testing"
)

func TestRefreshDefaultProxy(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(key, "")
	}
	t.Setenv("HTTP_PROXY", "http://old-proxy.example:8080")
	t.Setenv("HTTPS_PROXY", "http://old-proxy.example:8080")
	request, err := http.NewRequest(http.MethodGet, "https://sts.mtls.googleapis.com/v1/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a loads request consulting the globally cached proxy function.
	if _, err := http.ProxyFromEnvironment(request); err != nil {
		t.Fatal(err)
	}
	oldProxy := http.ProxyURL(&url.URL{Scheme: "http", Host: "old-proxy.example:8080"})
	before := &http.Transport{Proxy: oldProxy, MaxIdleConns: 42}
	http.DefaultTransport = before
	if err := ApplyEnv(map[string]any{
		"HTTP_PROXY":  "http://http-proxy.example:8080",
		"HTTPS_PROXY": "http://https-proxy.example:8443",
		"NO_PROXY":    ".internal",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := RefreshDefaultProxy(); err != nil {
		t.Fatal(err)
	}
	// gcp_token creates its client by cloning this transport as well.
	after := http.DefaultTransport.(*http.Transport).Clone()
	if after.MaxIdleConns != before.MaxIdleConns {
		t.Fatal("transport settings were not preserved")
	}
	for _, tc := range []struct {
		url  string
		want string
	}{
		{"https://sts.mtls.googleapis.com/v1/token", "http://https-proxy.example:8443"},
		{"http://example.com", "http://http-proxy.example:8080"},
		{"https://service.internal", ""},
		{"http://localhost", ""},
	} {
		t.Run(tc.url, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			proxy, err := after.Proxy(req)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if proxy != nil {
				got = proxy.String()
			}
			if got != tc.want {
				t.Fatalf("proxy = %q, want %q", got, tc.want)
			}
		})
	}
	proxy, err := before.Proxy(request)
	if err != nil || proxy.String() != "http://old-proxy.example:8080" {
		t.Fatalf("existing transport changed: proxy %v, err %v", proxy, err)
	}
}
