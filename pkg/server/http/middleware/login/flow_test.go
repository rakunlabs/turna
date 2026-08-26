package login

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

func TestLoginStateCarriesRememberMeAndFlow(t *testing.T) {
	m := &Login{StateCookie: auth.Cookie{CookieName: "state", Path: "/"}}
	recorder := httptest.NewRecorder()
	m.SetState(recorder, "random-state", StateInfo{RememberMe: true, Flow: "flow-1"})

	result := recorder.Result()
	if len(result.Cookies()) != 1 {
		t.Fatalf("cookies = %d", len(result.Cookies()))
	}

	request := httptest.NewRequest(http.MethodGet, "https://example.com/callback?state=random-state", nil)
	request.AddCookie(result.Cookies()[0])
	checkRecorder := httptest.NewRecorder()
	info, err := m.CheckState(checkRecorder, request, "random-state")
	if err != nil {
		t.Fatalf("check state: %v", err)
	}
	if !info.RememberMe {
		t.Fatal("remember_me was not carried through state")
	}
	if info.Flow != "flow-1" {
		t.Fatalf("flow = %q, want the popup flow id back", info.Flow)
	}
}

// A login page opened inside a popup starts its own code flow. With one
// shared state cookie the nested flow overwrote the outer state and then
// deleted the cookie when its callback consumed it, so the outer callback
// failed with "state is not valid" and left the popup stranded.
func TestNestedCodeFlowKeepsOuterState(t *testing.T) {
	m := &Login{StateCookie: auth.Cookie{CookieName: "auth_state", Path: "/"}}

	outer := httptest.NewRecorder()
	m.SetState(outer, "outer-state", StateInfo{Flow: "outer"})

	inner := httptest.NewRecorder()
	m.SetState(inner, "inner-state", StateInfo{Flow: "inner"})

	browser := httptest.NewRequest(http.MethodGet, "https://example.com/callback", nil)
	for _, cookie := range append(outer.Result().Cookies(), inner.Result().Cookies()...) {
		browser.AddCookie(cookie)
	}

	// the inner popup completes first and consumes its own state
	innerCheck := httptest.NewRecorder()
	innerInfo, err := m.CheckState(innerCheck, browser, "inner-state")
	if err != nil {
		t.Fatalf("inner check state: %v", err)
	}
	if innerInfo.Flow != "inner" {
		t.Fatalf("inner flow = %q", innerInfo.Flow)
	}

	// applying what the inner callback wrote back must not disturb the outer flow
	dropped := map[string]struct{}{}
	for _, cookie := range innerCheck.Result().Cookies() {
		if cookie.MaxAge < 0 {
			dropped[cookie.Name] = struct{}{}
		}
	}

	remaining := httptest.NewRequest(http.MethodGet, "https://example.com/callback", nil)
	for _, cookie := range browser.Cookies() {
		if _, ok := dropped[cookie.Name]; ok {
			continue
		}

		remaining.AddCookie(cookie)
	}

	outerInfo, err := m.CheckState(httptest.NewRecorder(), remaining, "outer-state")
	if err != nil {
		t.Fatalf("outer check state after nested flow: %v", err)
	}
	if outerInfo.Flow != "outer" {
		t.Fatalf("outer flow = %q", outerInfo.Flow)
	}
}

// The success marker tells one waiting opener that its own popup finished.
// A single shared cookie made every nested level resolve at once: the
// outermost login page closed the intermediate window mid-redirect and
// navigated away without ever getting a session.
func TestSuccessCookieIsScopedToFlow(t *testing.T) {
	m := &Login{SuccessCookie: auth.Cookie{CookieName: "auth_verify", Path: "/", MaxAge: 60}}

	recorder := httptest.NewRecorder()
	m.SetSuccess(recorder, "inner", "true")

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	if cookies[0].Name != "auth_verify_inner" {
		t.Fatalf("cookie name = %q, want the flow scoped name", cookies[0].Name)
	}

	request := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	request.AddCookie(cookies[0])

	if _, err := m.GetSuccess(*request, "outer"); err == nil {
		t.Fatal("an inner completion must not satisfy the outer flow")
	}

	got, err := m.GetSuccess(*request, "inner")
	if err != nil {
		t.Fatalf("get success: %v", err)
	}
	if got != "true" {
		t.Fatalf("success = %q", got)
	}
}

func TestSanitizeFlowID(t *testing.T) {
	for _, valid := range []string{"abc", "A-b_0", strings.Repeat("a", flowIDMaxLength)} {
		if got := sanitizeFlowID(valid); got != valid {
			t.Errorf("sanitizeFlowID(%q) = %q", valid, got)
		}
	}

	// anything that could shape a cookie name falls back to the shared marker
	for _, invalid := range []string{"", "a b", "a;b", "a=b", "üü", strings.Repeat("a", flowIDMaxLength+1)} {
		if got := sanitizeFlowID(invalid); got != "" {
			t.Errorf("sanitizeFlowID(%q) = %q, want empty", invalid, got)
		}
	}

	m := &Login{SuccessCookie: auth.Cookie{CookieName: "auth_verify", Path: "/"}}
	if got := m.successCookie("bad name").CookieName; got != "auth_verify" {
		t.Fatalf("rejected flow id must reuse the shared cookie, got %q", got)
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
		"window.opener.focus()",
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
