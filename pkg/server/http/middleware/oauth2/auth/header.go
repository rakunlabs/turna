package auth

import (
	"net/http"
	"net/url"

	"github.com/worldline-go/turna/pkg/server/http/middleware/session"
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
		query := r.URL.Query()
		if clientID != "" {
			query.Add("client_id", clientID)
		}
		if clientSecret != "" {
			query.Add("client_secret", clientSecret)
		}
		r.URL.RawQuery = query.Encode()
	}
}

// SetBearerAuth sets the Authorization header to use Bearer token.
func SetBearerAuth(r *http.Request, token string) {
	r.Header.Add("Authorization", "Bearer "+token)
}
