package filemanager

import (
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// crossSiteForm reports a cross-site browser POST with a body type an HTML
// form can send without a CORS preflight; with cookie auth another site
// could create, move or upload files otherwise.
func crossSiteForm(r *http.Request) bool {
	if r.Method != http.MethodPost || !crossSite(r) {
		return false
	}

	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	if contentType == "" {
		return true
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return true
	}

	switch mediaType {
	case "application/x-www-form-urlencoded", "multipart/form-data", "text/plain":
		return true
	}

	return false
}

// crossSite prefers Fetch Metadata and falls back to comparing Origin with
// the request host. No browser signal means a non-browser caller.
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
