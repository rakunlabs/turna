package config

import (
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/net/http/httpproxy"
)

// RefreshDefaultProxy snapshots the current proxy environment after loads and
// ApplyEnv, bypassing net/http's process-wide ProxyFromEnvironment cache.
// Replace the default transport with a clone so existing clients are untouched.
// Call only during startup, before creating middleware clients.
func RefreshDefaultProxy() error {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("cannot refresh proxy: default HTTP transport is not *http.Transport")
	}

	proxy := httpproxy.FromEnvironment().ProxyFunc()
	updated := transport.Clone()
	updated.Proxy = func(req *http.Request) (*url.URL, error) {
		return proxy(req.URL)
	}
	http.DefaultTransport = updated
	return nil
}
