package login

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/worldline-go/turna/pkg/server/http/middleware/session"
)

type Redirect struct {
	// BaseURL is the base URL to use for the redirect.
	// Default is the request Host with checking the X-Forwarded-Host header.
	BaseURL string `cfg:"base_url"`
	// Schema is the default schema to use for the redirect if no schema is provided.
	// Default is the https schema.
	Schema string `cfg:"schema"`
}

func (m *Login) AuthCodeRedirectURL(r *http.Request, providerName string) (string, error) {
	if m.Redirect.BaseURL == "" {
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
				if m.Redirect.Schema != "" {
					r.URL.Scheme = m.Redirect.Schema
				} else {
					r.URL.Scheme = "https"
				}
			}
		}
	} else {
		urlParsed, err := url.Parse(m.Redirect.BaseURL)
		if err != nil {
			return "", err
		}

		r.URL.Scheme = urlParsed.Scheme
		r.URL.Host = urlParsed.Host
	}

	r.URL.Path = path.Join(m.pathFixed.Code, providerName)

	r.URL.RawQuery = ""

	return r.URL.String(), nil
}

func (m *Login) AuthCodeURL(r *http.Request, state, providerName string, oauth2 *session.Oauth2) (string, error) {
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
