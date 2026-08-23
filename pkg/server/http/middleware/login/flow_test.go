package login

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

func TestLoginStateCarriesRememberMe(t *testing.T) {
	m := &Login{StateCookie: auth.Cookie{CookieName: "state", Path: "/"}}
	recorder := httptest.NewRecorder()
	m.SetState(recorder, "random-state", true)

	result := recorder.Result()
	if len(result.Cookies()) != 1 {
		t.Fatalf("cookies = %d", len(result.Cookies()))
	}

	request := httptest.NewRequest(http.MethodGet, "https://example.com/callback?state=random-state", nil)
	request.AddCookie(result.Cookies()[0])
	checkRecorder := httptest.NewRecorder()
	rememberMe, err := m.CheckState(checkRecorder, request, "random-state")
	if err != nil {
		t.Fatalf("check state: %v", err)
	}
	if !rememberMe {
		t.Fatal("remember_me was not carried through state")
	}
}

func TestDisableRememberMeForcesOff(t *testing.T) {
	m := &Login{Info: Info{DisableRememberMe: true}}
	m.session = &session.Session{Provider: map[string]session.Provider{}}

	if m.rememberMe(true) {
		t.Fatal("disable_remember_me must override a requested remember_me")
	}

	if got := m.informationUIResponse(); !got.DisableRememberMe {
		t.Fatal("methods response must advertise disable_remember_me")
	}

	enabled := &Login{}
	if !enabled.rememberMe(true) || enabled.rememberMe(false) {
		t.Fatal("without the switch, the requested value must pass through")
	}
}

func TestWriteCodeFlowSuccess(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeCodeFlowSuccess(recorder)

	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		"Sign-in complete",
		`postMessage("turna:login:success"`,
		"window.close()",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response does not contain %q", want)
		}
	}

	// the popup must never navigate itself; when window.close() is blocked
	// (e.g. COOP providers) a location change would send the still-open popup
	// back to the login page.
	for _, forbidden := range []string{"location.replace(", "location.assign(", "location.href"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("success page must not navigate the popup window (found %q)", forbidden)
		}
	}
}
