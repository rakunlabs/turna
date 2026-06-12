package login

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// IssuerPasswordToken runs the password grant in-process against a registered
// issuer (auth middleware) instead of calling token_url over HTTP.
func (m *Login) IssuerPasswordToken(ctx context.Context, issuerName, username, password string, oauth2 *session.Oauth2) ([]byte, int, error) {
	issuer := session.IssuerRegistry.Get(issuerName)
	if issuer == nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("issuer %q not found", issuerName)
	}

	uValues := url.Values{
		"grant_type": {"password"},
		"username":   {username},
		"password":   {password},
		"client_id":  {oauth2.ClientID},
	}
	if oauth2.ClientSecret != "" {
		uValues.Set("client_secret", oauth2.ClientSecret)
	}
	if len(oauth2.Scopes) > 0 {
		uValues.Set("scope", strings.Join(oauth2.Scopes, " "))
	}

	body, statusCode, err := issuer.IssueToken(ctx, uValues)
	if err != nil {
		return nil, statusCode, err
	}

	if statusCode < 200 || statusCode > 299 {
		return nil, statusCode, errors.New(string(body))
	}

	return body, statusCode, nil
}

// RemotePasskeyToken proxies a WebAuthn begin/finish payload to a remote
// auth middleware's passkey endpoint. The original request's host/scheme is
// forwarded so the remote side derives the relying party from the login page.
// Non-2xx responses are passed through to the caller, not turned into errors.
func (m *Login) RemotePasskeyToken(r *http.Request, passkeyURL string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, passkeyURL, bytes.NewReader(body))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}

	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	req.Header.Set("X-Forwarded-Host", host)
	req.Header.Set("X-Forwarded-Proto", scheme)

	var respBody []byte
	statusCode := 0
	if err := m.client.Do(req, func(res *http.Response) error {
		var err error
		// 1MB limit
		respBody, err = io.ReadAll(io.LimitReader(res.Body, 1<<20))
		statusCode = res.StatusCode

		return err
	}); err != nil {
		return nil, http.StatusInternalServerError, err
	}

	return respBody, statusCode, nil
}

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
