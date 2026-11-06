package login

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
)

func TestStatusHandlerRejectsScriptInCookieName(t *testing.T) {
	m := &Login{SuccessCookie: auth.Cookie{CookieName: "auth_verify"}}
	if err := m.SetFiles(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	m.StatusHandler(rec, httptest.NewRequest(http.MethodGet, `/auth/status/x%22)%3Balert(1)%3B(%22`, nil))

	body := rec.Body.String()
	if strings.Contains(body, "alert(1)") {
		t.Fatalf("cookie name injected: %s", body)
	}

	if !strings.Contains(body, "auth_verify") {
		t.Fatalf("default cookie name missing: %s", body)
	}

	rec = httptest.NewRecorder()
	m.StatusHandler(rec, httptest.NewRequest(http.MethodGet, "/auth/status/my_cookie", nil))

	if !strings.Contains(rec.Body.String(), `getCookieByName("my_cookie")`) {
		t.Fatalf("cookie name not rendered: %s", rec.Body.String())
	}
}
