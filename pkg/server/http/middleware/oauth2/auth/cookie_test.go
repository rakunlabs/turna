package auth

import (
	"net/http/httptest"
	"testing"
)

func TestCookieDefaults(t *testing.T) {
	rec := httptest.NewRecorder()
	SetCookie(rec, "v", &Cookie{CookieName: "c", Path: "/"})

	c := rec.Result().Cookies()[0]
	if !c.Secure || !c.HttpOnly {
		t.Fatalf("secure=%v httpOnly=%v, want both by default", c.Secure, c.HttpOnly)
	}

	rec = httptest.NewRecorder()
	RemoveCookie(rec, &Cookie{CookieName: "c", Secure: Bool(false), HttpOnly: Bool(false)})

	c = rec.Result().Cookies()[0]
	if c.Secure || c.HttpOnly || c.MaxAge != -1 {
		t.Fatalf("explicit false not honored: %+v", c)
	}
}
