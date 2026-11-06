package auth

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// AuthAdd is a function to set Authorization header.
//   - Style must be AuthHeaderStyleBasic or AuthHeaderStyleBearerSecret, otherwise it does nothing.
//   - Default style is AuthHeaderStyleBasic.
func AuthAdd(r *http.Request, clientID, clientSecret string, style session.AuthHeaderStyle) {
	if r == nil {
		return
	}

	switch style {
	case session.AuthHeaderStyleBasic:
		r.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	case session.AuthHeaderStyleBearerSecret:
		SetBearerAuth(r, clientSecret)
	case session.AuthHeaderStyleParams:
		addFormParams(r, clientID, clientSecret)
	}
}

// addFormParams adds the client credentials to the form body of a token
// request (RFC 6749 section 2.3.1); in the URL they end up in access logs.
func addFormParams(r *http.Request, clientID, clientSecret string) {
	var values url.Values
	if r.Body != nil && r.Body != http.NoBody {
		raw, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			return
		}

		if values, err = url.ParseQuery(string(raw)); err != nil {
			return
		}
	}

	if values == nil {
		values = url.Values{}
	}

	if clientID != "" {
		values.Set("client_id", clientID)
	}
	if clientSecret != "" {
		values.Set("client_secret", clientSecret)
	}

	body := values.Encode()
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}
}

// SetBearerAuth sets the Authorization header to use Bearer token.
func SetBearerAuth(r *http.Request, token string) {
	r.Header.Add("Authorization", "Bearer "+token)
}
