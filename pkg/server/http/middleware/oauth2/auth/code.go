package auth

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/worldline-go/klient"

	"github.com/worldline-go/turna/pkg/server/http/middleware/session"
)

type Code struct {
	// BaseURL is the base URL to use for the redirect.
	// Default is the request Host with checking the X-Forwarded-Host header.
	BaseURL string `cfg:"base_url"`
	// Schema is the default schema to use for the redirect if no schema is provided.
	// Default is the https schema.
	Schema string `cfg:"schema"`
	// Path is the path to use for the redirect.
	Path string `cfg:"path"`

	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`

	client *klient.Client `cfg:"-"`
}

func (m *Code) Init() error {
	client, err := klient.New(
		klient.WithDisableBaseURLCheck(true),
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
		klient.WithDisableRetry(true),
		klient.WithDisableEnvValues(true),
		klient.WithLogger(slog.Default()),
	)
	if err != nil {
		return err
	}

	m.client = client

	return nil
}

func (m *Code) AuthCodeRedirectURL(r *http.Request, providerName string) (string, error) {
	if m.BaseURL == "" {
		// check headers of X-Forwarded-Proto and X-Forwarded-Host
		// if they are set, use them to build the redirect uri

		proto := r.Header.Get("X-Forwarded-Proto")
		host := r.Header.Get("X-Forwarded-Host")

		if proto != "" && host != "" {
			r.URL.Scheme = proto
			r.URL.Host = host
		} else {
			// check the host header
			host := r.Host
			if host != "" {
				r.URL.Host = host
				if m.Schema != "" {
					r.URL.Scheme = m.Schema
				} else {
					r.URL.Scheme = "https"
				}
			}
		}
	} else {
		urlParsed, err := url.Parse(m.BaseURL)
		if err != nil {
			return "", err
		}

		r.URL.Scheme = urlParsed.Scheme
		r.URL.Host = urlParsed.Host
	}

	r.URL.Path = path.Join(m.Path, providerName)
	r.URL.RawQuery = ""

	return r.URL.String(), nil
}

func (m *Code) AuthCodeURL(r *http.Request, state, providerName string, oauth2 *session.Oauth2) (string, error) {
	if oauth2 == nil {
		return "", fmt.Errorf("provider %q has no oauth2", providerName)
	}

	authCodeRedirectURL, err := m.AuthCodeRedirectURL(r, providerName)
	if err != nil {
		return "", err
	}

	urlParsed, err := url.Parse(oauth2.AuthURL)
	if err != nil {
		return "", err
	}

	data := urlParsed.Query()
	data.Add("response_type", "code")
	data.Add("state", state)
	data.Add("redirect_uri", authCodeRedirectURL)
	data.Add("client_id", oauth2.ClientID)
	if len(oauth2.Scopes) > 0 {
		data.Add("scope", strings.Join(oauth2.Scopes, " "))
	}

	urlParsed.RawQuery = data.Encode()
	redirect := urlParsed.String()

	return redirect, nil
}

func (m *Code) CodeToken(ctx context.Context, r *http.Request, code, providerName string, oauth2 *session.Oauth2) ([]byte, int, error) {
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
	AuthAdd(req, oauth2.ClientID, oauth2.ClientSecret, oauth2.AuthHeaderStyle)

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

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
