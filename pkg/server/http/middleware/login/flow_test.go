package login

import (
	"net/http/httptest"
	"strings"
	"testing"
)

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
