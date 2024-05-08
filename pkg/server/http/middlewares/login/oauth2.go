package login

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middlewares/session"
)

func (m *Login) PasswordToken(ctx context.Context, username, password string, oauth2 *session.Oauth2) ([]byte, int, error) {
	uValues := url.Values{
		"grant_type": {"password"},
		"username":   {username},
		"password":   {password},
	}
	if len(oauth2.Scopes) > 0 {
		uValues.Set("scope", strings.Join(oauth2.Scopes, " "))
	}

	encodedData := uValues.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauth2.TokenURL, strings.NewReader(encodedData))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// set if style is params
	AuthAdd(req, oauth2.ClientID, oauth2.ClientSecret, oauth2.AuthHeaderStyle)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")

	var body []byte
	statusCode := 0
	if err := m.client.Do(req, func(r *http.Response) error {
		// 1MB limit
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return err
		}

		if statusCode = r.StatusCode; statusCode < 200 || statusCode > 299 {
			return fmt.Errorf(string(body))
		}

		return nil
	}); err != nil {
		return nil, statusCode, err
	}

	return body, statusCode, nil
}

// CodeToken get token and set the cookie/session.
func (m *Login) CodeToken(c echo.Context, code, providerName string, oauth2 *session.Oauth2) ([]byte, int, error) {
	ctx := c.Request().Context()
	redirectURI, err := m.AuthCodeRedirectURL(c.Request(), providerName)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	uValues := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {code},
	}

	if redirectURI != "" {
		uValues.Set("redirect_uri", redirectURI)
	}

	encodedData := uValues.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauth2.TokenURL, strings.NewReader(encodedData))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	// set if style is params
	AuthAdd(req, oauth2.ClientID, oauth2.ClientSecret, oauth2.AuthHeaderStyle)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")

	var body []byte
	statusCode := 0
	if err := m.client.Do(req, func(r *http.Response) error {
		// 1MB limit
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return err
		}

		if statusCode = r.StatusCode; statusCode < 200 || statusCode > 299 {
			return fmt.Errorf(string(body))
		}

		return nil
	}); err != nil {
		return nil, statusCode, err
	}

	return body, statusCode, nil
}

// AuthAdd is a function to set Authorization header.
//
// Style must be AuthHeaderStyleBasic or AuthHeaderStyleBearerSecret, otherwise it does nothing.
//
// Default style is AuthHeaderStyleBasic.
func AuthAdd(req *http.Request, clientID, clientSecret string, style session.AuthHeaderStyle) {
	if req == nil {
		return
	}

	switch style {
	case session.AuthHeaderStyleBasic:
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	case session.AuthHeaderStyleBearerSecret:
		SetBearerAuth(req, clientSecret)
	case session.AuthHeaderStyleParams:
		query := req.URL.Query()
		if clientID != "" {
			query.Add("client_id", clientID)
		}
		if clientSecret != "" {
			query.Add("client_secret", clientSecret)
		}
		req.URL.RawQuery = query.Encode()
	}
}

// SetBearerAuth sets the Authorization header to use Bearer token.
func SetBearerAuth(r *http.Request, token string) {
	r.Header.Add("Authorization", "Bearer "+token)
}
