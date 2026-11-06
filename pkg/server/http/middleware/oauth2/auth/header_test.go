package auth

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

func TestAuthAddParamsInBody(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://idp/token", strings.NewReader("grant_type=password"))
	if err != nil {
		t.Fatal(err)
	}

	AuthAdd(req, "id", "secret", session.AuthHeaderStyleParams)

	if req.URL.RawQuery != "" {
		t.Fatalf("credentials in url: %q", req.URL.RawQuery)
	}

	raw, _ := io.ReadAll(req.Body)
	values, _ := url.ParseQuery(string(raw))
	if values.Get("grant_type") != "password" || values.Get("client_id") != "id" || values.Get("client_secret") != "secret" {
		t.Fatalf("body = %q", raw)
	}

	if req.ContentLength != int64(len(raw)) {
		t.Fatalf("content length = %d, body %d", req.ContentLength, len(raw))
	}
}
