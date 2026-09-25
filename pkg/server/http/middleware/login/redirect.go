package login

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// pkceParams reads an optional RFC 7636 challenge from an authorization
// request; an empty method defaults to "plain".
func pkceParams(query url.Values) (string, string, error) {
	challenge := query.Get("code_challenge")
	method := query.Get("code_challenge_method")

	if challenge == "" {
		if method != "" {
			return "", "", fmt.Errorf("code_challenge_method without code_challenge")
		}

		return "", "", nil
	}

	switch method {
	case "":
		method = "plain"
	case "plain", "S256":
	default:
		return "", "", fmt.Errorf("code_challenge_method %q not supported", method)
	}

	return challenge, method, nil
}

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

func (m *Login) IsValidRedirectURI(redirectURI string) bool {
	checked := false
	if redirectURI == "" {
		return checked
	}

	if len(m.RedirectWhiteList) > 0 {
		for _, v := range m.RedirectWhiteList {
			if strings.HasPrefix(redirectURI, v) {
				checked = true

				break
			}
		}
	} else {
		checked = true
	}

	return checked
}

func (m *Login) IsForRedirection(r *http.Request) bool {
	if responseType := r.URL.Query().Get("response_type"); responseType == "code" {
		return m.IsValidRedirectURI(r.URL.Query().Get("redirect_uri"))
	}

	return false
}

func (m *Login) AuthCodeReturn(w http.ResponseWriter, r *http.Request, customClaim *claims.Custom) {
	query := r.URL.Query()
	state := query.Get("state")
	scope := query.Get("scope")

	redirectURI := query.Get("redirect_uri")

	// check redirect uri whitelist
	if !m.IsValidRedirectURI(redirectURI) {
		writeError(w, http.StatusForbidden, "redirect_uri is not allowed")

		return
	}

	// get new code
	var alias string
	for _, k := range []string{"preferred_username", "email", "name"} {
		if vAlias, _ := customClaim.Map[k].(string); vAlias != "" {
			alias = vAlias

			break
		}
	}

	if alias == "" {
		writeError(w, http.StatusForbidden, "alias is empty")

		return
	}

	// PKCE (RFC 7636) is passed through so the token endpoint can verify it
	// instead of silently dropping a challenge the caller asked for.
	codeChallenge, codeChallengeMethod, err := pkceParams(query)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	// Bind the code to the requesting client and redirect target; the auth
	// middleware token endpoint rejects codes without these bindings
	// (RFC 6749 §4.1.3).
	code, err := m.codeGen(r, store.Code{
		Alias:               alias,
		Scope:               strings.Fields(scope),
		Nonce:               query.Get("nonce"),
		ClientID:            query.Get("client_id"),
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
	})
	if err != nil {
		slog.Error("cannot generate authorization code", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to generate code")

		return
	}

	// redirect to the redirect uri
	urlParsed, err := url.Parse(redirectURI)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse redirect uri")

		return
	}

	q := url.Values{}
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}

	urlParsed.RawQuery = q.Encode()

	httputil.Redirect(w, http.StatusTemporaryRedirect, urlParsed.String())
}

// codeIssuer is implemented by an in-process auth middleware that can store an
// authorization code where its own token endpoint redeems it.
type codeIssuer interface {
	IssueAuthorizationCode(ctx context.Context, code store.Code) (string, error)
}

// codeGen stores the authorization code where the token endpoint will look
// for it. With an in-process auth middleware that is the auth middleware's
// code store (database, memory or redis); login's own store is only a
// fallback for a remote token endpoint sharing the same Redis.
func (m *Login) codeGen(r *http.Request, code store.Code) (string, error) {
	if issuer := m.codeIssuer(r); issuer != nil {
		return issuer.IssueAuthorizationCode(r.Context(), code)
	}

	return m.store.CodeGen(r.Context(), code)
}

// codeIssuer picks the configured auth_middleware, else the auth middleware
// of the provider the user is logged in with, else the only in-process auth
// middleware among the session providers.
func (m *Login) codeIssuer(r *http.Request) codeIssuer {
	if m.AuthMiddleware != "" {
		issuer, _ := session.IssuerRegistry.Get(m.AuthMiddleware).(codeIssuer)

		return issuer
	}

	if m.session == nil {
		return nil
	}

	name := ""
	if m.session.GetStore() != nil {
		if _, providerName, err := m.session.GetTokenData(r); err == nil {
			if provider, ok := m.session.GetProvider(providerName); ok {
				name = provider.AuthMiddleware
			}
		}
	}

	if name == "" {
		for _, provider := range m.session.Providers() {
			if provider.AuthMiddleware == "" {
				continue
			}
			if name != "" && name != provider.AuthMiddleware {
				// several auth middlewares and no session provider to choose by
				return nil
			}
			name = provider.AuthMiddleware
		}
	}

	if name == "" {
		return nil
	}

	issuer, _ := session.IssuerRegistry.Get(name).(codeIssuer)

	return issuer
}
