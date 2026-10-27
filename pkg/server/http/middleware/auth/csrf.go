package auth

import (
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// csrfGuard protects the cookie-authenticated /v1 plane against cross-site
// request forgery without breaking legitimate callers:
//
//   - Server-to-server and CLI callers send neither Origin nor
//     Sec-Fetch-Site and are never affected.
//   - Browser UIs on other origins call with a JSON body; a cross-origin JSON
//     request needs a CORS preflight, so the CORS policy already decides it.
//   - Only what a plain HTML form (or a no-preflight fetch) can send
//     cross-site is refused: a state-changing request with a form,
//     multipart, text/plain or missing body type, coming from another site.
//
// The /oauth2 and /saml planes are left out on purpose: token, signup and
// SAML ACS requests are cross-site form posts by design and authenticate
// the client themselves; the consent form carries its own token.
func (m *Auth) csrfGuard(next http.Handler) http.Handler {
	prefix := strings.TrimSuffix(m.PrefixPath, "/") + "/v1/"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, prefix) && csrfSuspect(r) {
			httputil.HandleError(w, httputil.NewError(
				"cross-site request refused; send a JSON body (application/json)", nil, http.StatusForbidden))

			return
		}

		next.ServeHTTP(w, r)
	})
}

// csrfSuspect reports a cross-site browser POST whose body type a form could
// have produced. PUT, PATCH and DELETE always need a CORS preflight
// cross-origin, so only POST can be forged without one.
func csrfSuspect(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}

	return crossSiteBrowserRequest(r) && corsSimpleContentType(r.Header.Get("Content-Type"))
}

// crossSiteBrowserRequest prefers Fetch Metadata and falls back to comparing
// Origin with the request host. No browser signal means a non-browser caller.
func crossSiteBrowserRequest(r *http.Request) bool {
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
	if origin == "null" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return true
	}

	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}

	return !strings.EqualFold(u.Host, host)
}

// corsSimpleContentType reports body types a cross-site request can carry
// without a CORS preflight.
func corsSimpleContentType(contentType string) bool {
	if strings.TrimSpace(contentType) == "" {
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
