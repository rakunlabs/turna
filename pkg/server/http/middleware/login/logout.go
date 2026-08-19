package login

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/exp/slog"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// revokeSessionTokens best-effort revokes the session's refresh and access
// tokens at the issuer so they cannot be replayed after logout: in-process
// when the provider is issuer-backed (auth_middleware), otherwise over the
// provider's oauth2.revocation_url.
func (m *Login) revokeSessionTokens(r *http.Request) {
	token, providerName, err := m.session.GetTokenData(r)
	if err != nil {
		return
	}

	provider, ok := m.session.Provider[providerName]
	if !ok {
		return
	}

	tokens := []string{token.RefreshToken, token.AccessToken}

	if provider.AuthMiddleware != "" {
		revoker, ok := session.IssuerRegistry.Get(provider.AuthMiddleware).(session.InfRevoker)
		if !ok {
			return
		}

		for _, t := range tokens {
			if t == "" {
				continue
			}

			if err := revoker.RevokeToken(r.Context(), t); err != nil {
				slog.Debug("logout revoke failed", "err", err.Error())
			}
		}

		return
	}

	if provider.Oauth2 == nil || provider.Oauth2.RevocationURL == "" {
		return
	}

	for _, t := range tokens {
		if t == "" {
			continue
		}

		form := url.Values{"token": {t}, "client_id": {provider.Oauth2.ClientID}}
		if provider.Oauth2.ClientSecret != "" {
			form.Set("client_secret", provider.Oauth2.ClientSecret)
		}

		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, provider.Oauth2.RevocationURL, strings.NewReader(form.Encode()))
		if err != nil {
			continue
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		if err := m.client.Do(req, func(resp *http.Response) error {
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("revocation endpoint answered %s", resp.Status)
			}

			return nil
		}); err != nil {
			slog.Debug("logout revoke failed", "err", err.Error())
		}
	}
}

func (m *Login) Logout(w http.ResponseWriter, r *http.Request) {
	// invalidate the tokens at the issuer before the session is deleted
	m.revokeSessionTokens(r)

	token, oauth2, err := m.session.GetToken(r)
	if err == nil && oauth2.LogoutURL != "" {
		if token.IDToken == "" {
			slog.Error("id_token is empty")
		} else {
			logoutURL, err := url.Parse(oauth2.LogoutURL)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to parse logout URL")

				return
			}

			q := logoutURL.Query()
			q.Set("id_token_hint", token.IDToken)
			q.Set("client_id", oauth2.ClientID)
			logoutURL.RawQuery = q.Encode()

			req := &http.Request{
				Method: http.MethodGet,
				URL:    logoutURL,
			}

			if err := m.client.Do(req, func(resp *http.Response) error {
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					return fmt.Errorf("failed to logout: %s", resp.Status)
				}

				return nil
			}); err != nil {
				slog.Error("failed to logout", "err", err.Error())
			}
		}
	}

	m.RemoveSuccess(w)

	m.session.RedirectToLogin(w, r, false, true)
}
