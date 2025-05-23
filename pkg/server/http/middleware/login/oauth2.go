package login

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
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
	auth.AuthAdd(req, oauth2.ClientID, oauth2.ClientSecret, oauth2.AuthHeaderStyle)

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
			return errors.New(string(body))
		}

		return nil
	}); err != nil {
		return nil, statusCode, err
	}

	return body, statusCode, nil
}

// CodeToken get token and set the cookie/session.
func (m *Login) CodeToken(r *http.Request, code, providerName string, oauth2 *session.Oauth2) ([]byte, int, error) {
	ctx := r.Context()
	redirectURI, err := m.AuthCodeRedirectURL(r, providerName)
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
	auth.AuthAdd(req, oauth2.ClientID, oauth2.ClientSecret, oauth2.AuthHeaderStyle)

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
			return errors.New(string(body))
		}

		return nil
	}); err != nil {
		return nil, statusCode, err
	}

	return body, statusCode, nil
}
