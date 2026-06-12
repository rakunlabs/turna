package login

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// providerSignup resolves the optional signup support of a provider.
// In-process providers expose it through the issuer's InfSignup interface;
// remote providers are detected by the configured signup/reset URLs.
func providerSignup(provider session.Provider) (session.SignupFeatures, session.InfSignup) {
	if provider.AuthMiddleware != "" {
		if issuer, ok := session.IssuerRegistry.Get(provider.AuthMiddleware).(session.InfSignup); ok {
			return issuer.SignupFeatures(), issuer
		}

		return session.SignupFeatures{}, nil
	}

	features := session.SignupFeatures{}
	if provider.Oauth2 != nil {
		features.Signup = provider.Oauth2.SignupURL != ""
		features.PasswordReset = provider.Oauth2.PasswordResetURL != ""
	}

	return features, nil
}

// signupRemoteURL maps an action to the remote auth endpoint.
func signupRemoteURL(oauth2 *session.Oauth2, action string) string {
	switch action {
	case session.SignupActionSignup:
		return oauth2.SignupURL
	case session.SignupActionVerify:
		if oauth2.SignupURL == "" {
			return ""
		}

		return strings.TrimSuffix(oauth2.SignupURL, "/") + "/verify"
	case session.SignupActionReset:
		return oauth2.PasswordResetURL
	case session.SignupActionResetConfirm:
		if oauth2.PasswordResetURL == "" {
			return ""
		}

		return strings.TrimSuffix(oauth2.PasswordResetURL, "/") + "/confirm"
	}

	return ""
}

// SignupFlow proxies signup/verify/password-reset requests to the provider's
// auth middleware, injecting the provider client credentials so the SPA never
// sees them. Responses are passed through unchanged.
func (m *Login) SignupFlow(w http.ResponseWriter, r *http.Request, action string) {
	pathSplit := strings.Split(r.URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		writeError(w, http.StatusInternalServerError, "session middleware not found")

		return
	}

	provider, ok := sessionM.Provider[providerName]
	if !ok || provider.Oauth2 == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("provider %q not found", providerName))

		return
	}

	features, issuer := providerSignup(provider)
	switch action {
	case session.SignupActionSignup, session.SignupActionVerify:
		if !features.Signup {
			writeError(w, http.StatusNotFound, "signup is not enabled")

			return
		}
	case session.SignupActionReset, session.SignupActionResetConfirm:
		if !features.PasswordReset {
			writeError(w, http.StatusNotFound, "password reset is not enabled")

			return
		}
	}

	// decode the payload and inject the provider's client credentials
	payload := map[string]any{}
	if err := httputil.DecodeJSON(r.Body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	payload["client_id"] = provider.Oauth2.ClientID
	if provider.Oauth2.ClientSecret != "" {
		payload["client_secret"] = provider.Oauth2.ClientSecret
	}

	body, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	var (
		respBody   []byte
		statusCode int
	)

	if issuer != nil {
		respBody, statusCode, err = issuer.SignupAction(r.Context(), action, body)
	} else {
		remoteURL := signupRemoteURL(provider.Oauth2, action)
		if remoteURL == "" {
			writeError(w, http.StatusNotFound, "signup endpoint is not configured")

			return
		}

		respBody, statusCode, err = m.remoteSignup(r, remoteURL, body)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(respBody)
}

// remoteSignup posts the payload to a remote auth middleware endpoint;
// non-2xx responses are passed through, not turned into errors.
func (m *Login) remoteSignup(r *http.Request, remoteURL string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, remoteURL, bytes.NewReader(body))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

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
