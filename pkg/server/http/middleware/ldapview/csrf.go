package ldapview

import (
	"net/http"
	"net/url"
	"strings"
)

// crossSite prefers Fetch Metadata and falls back to comparing Origin with
// the request host. No browser signal means a non-browser caller.
// The session cookie is SameSite=Strict already; this also stops a
// cross-site page from logging the browser in to a server of its choice.
func crossSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return false
	case "same-site", "cross-site":
		return true
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return true
	}

	return !strings.EqualFold(u.Host, r.Host)
}
