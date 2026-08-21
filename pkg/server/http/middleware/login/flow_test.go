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
		`window.location.replace("/")`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response does not contain %q", want)
		}
	}
}
