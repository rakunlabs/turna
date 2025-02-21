package login

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/model"
	"golang.org/x/exp/slog"
)

func (m *Login) Logout(w http.ResponseWriter, r *http.Request) {
	token, oauth2, err := m.session.GetToken(r)
	if err == nil && oauth2.LogoutURL != "" {
		if token.IDToken == "" {
			slog.Error("id_token is empty")
		} else {
			logoutURL, err := url.Parse(oauth2.LogoutURL)
			if err != nil {
				httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: "failed to parse logout URL"})

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

	m.session.RedirectToLogin(w, r, false, true)
}
