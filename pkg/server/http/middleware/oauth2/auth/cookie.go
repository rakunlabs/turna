package auth

import (
	"net/http"
)

type Cookie struct {
	// CookieName is the name of the cookie.
	CookieName string `cfg:"cookie_name"`
	// MaxAge the number of seconds until the cookie expires.
	MaxAge int `cfg:"max_age"`
	// Path that must exist in the requested URL for the browser to send the Cookie header.
	Path string `cfg:"path"`
	// Domain for defines the host to which the cookie will be sent.
	Domain string `cfg:"domain"`
	// Secure to cookie only sent over HTTPS. Default true.
	Secure *bool `cfg:"secure"`
	// SameSite for Lax 2, Strict 3, None 4.
	SameSite http.SameSite `cfg:"same_site"`
	// HttpOnly for true for not accessible by JavaScript. Default true.
	HttpOnly *bool `cfg:"http_only"`
}

func boolOr(v *bool, def bool) bool {
	if v == nil {
		return def
	}

	return *v
}

// Bool returns a pointer to v for the optional cookie flags.
func Bool(v bool) *bool { return &v }

// GetSecure reports the effective Secure flag (default true).
func (c *Cookie) GetSecure() bool { return boolOr(c.Secure, true) }

// GetHttpOnly reports the effective HttpOnly flag (default true).
func (c *Cookie) GetHttpOnly() bool { return boolOr(c.HttpOnly, true) }

func SetCookie(w http.ResponseWriter, value string, cookie *Cookie) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookie.CookieName,
		Value:    value,
		Domain:   cookie.Domain,
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
		Secure:   cookie.GetSecure(),
		SameSite: cookie.SameSite,
		HttpOnly: cookie.GetHttpOnly(),
	})
}

func RemoveCookie(w http.ResponseWriter, cookie *Cookie) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookie.CookieName,
		Value:    "",
		Domain:   cookie.Domain,
		Path:     cookie.Path,
		MaxAge:   -1,
		Secure:   cookie.GetSecure(),
		SameSite: cookie.SameSite,
		HttpOnly: cookie.GetHttpOnly(),
	})
}
