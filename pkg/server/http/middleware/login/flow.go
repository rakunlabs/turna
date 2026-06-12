package login

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type TokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (m *Login) CodeFlowInit(w http.ResponseWriter, r *http.Request, providerName string) {
	m.RemoveSuccess(w)

	state, err := auth.NewState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	m.SetState(w, state)

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		writeError(w, http.StatusInternalServerError, "session middleware not found")

		return
	}

	provider := sessionM.Provider[providerName]
	if provider.Oauth2 == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("provider %q not found", providerName))

		return
	}

	authCodeURL, err := m.AuthCodeURL(r, state, providerName, provider.Oauth2)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	httputil.Redirect(w, http.StatusTemporaryRedirect, authCodeURL)
}

func (m *Login) CodeFlow(w http.ResponseWriter, r *http.Request) {
	pathSplit := strings.Split(r.URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		writeError(w, http.StatusInternalServerError, "session middleware not found")

		return
	}

	var oauth2 *session.Oauth2
	if v, ok := sessionM.Provider[providerName]; ok && v.Oauth2 != nil {
		oauth2 = v.Oauth2
	}

	if oauth2 == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("provider %q not found", providerName))

		return
	}

	// get the token from the request
	query := r.URL.Query()
	code := query.Get("code")
	if code == "" {
		m.CodeFlowInit(w, r, providerName)

		return
	}

	state := query.Get("state")
	// check state
	if m.CheckState(w, r, state) != nil {
		writeError(w, http.StatusUnauthorized, "state is not valid")

		return
	}

	// get token from provider
	data, statusCode, err := m.CodeToken(r, code, providerName, oauth2)
	if err != nil {
		respondUpstreamError(w, statusCode, err)

		return
	}

	if err := sessionM.SetToken(w, r, data, providerName); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	// pop-up close window
	m.SetSuccess(w, "true")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<script>window.close();</script>"))
}

func (m *Login) PasswordFlow(w http.ResponseWriter, r *http.Request) {
	pathSplit := strings.Split(r.URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	// save token in session
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		writeError(w, http.StatusInternalServerError, "session middleware not found")

		return
	}

	provider, providerOK := sessionM.Provider[providerName]

	var oauth2 *session.Oauth2
	if providerOK && provider.Oauth2 != nil {
		oauth2 = provider.Oauth2
	}

	if oauth2 == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("provider %q not found", providerName))

		return
	}

	var request TokenRequest

	if err := httputil.DecodeJSON(r.Body, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	// get Token from provider; in-process when the provider is backed by an
	// auth middleware, otherwise over HTTP with token_url.
	var (
		data       []byte
		statusCode int
		err        error
	)

	if provider.AuthMiddleware != "" {
		data, statusCode, err = m.IssuerPasswordToken(r.Context(), provider.AuthMiddleware, request.Username, request.Password, oauth2)
	} else {
		data, statusCode, err = m.PasswordToken(r.Context(), request.Username, request.Password, oauth2)
	}

	if err != nil {
		respondUpstreamError(w, statusCode, err)

		return
	}

	if err := sessionM.SetToken(w, r, data, providerName); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	httputil.NoContent(w, http.StatusNoContent)
}

// PasskeyFlow proxies WebAuthn begin/finish ceremonies to an in-process
// auth middleware; a finish response with tokens is stored in the session.
func (m *Login) PasskeyFlow(w http.ResponseWriter, r *http.Request) {
	pathSplit := strings.Split(r.URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		writeError(w, http.StatusInternalServerError, "session middleware not found")

		return
	}

	provider, ok := sessionM.Provider[providerName]
	if !ok || provider.Oauth2 == nil || (provider.AuthMiddleware == "" && provider.Oauth2.PasskeyURL == "") {
		writeError(w, http.StatusNotFound, fmt.Sprintf("provider %q not found", providerName))

		return
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
	if len(provider.Oauth2.Scopes) > 0 {
		payload["scope"] = strings.Join(provider.Oauth2.Scopes, " ")
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

	if provider.AuthMiddleware != "" {
		// in-process auth middleware
		passkeyIssuer, ok := session.IssuerRegistry.Get(provider.AuthMiddleware).(session.InfPasskey)
		if !ok {
			writeError(w, http.StatusInternalServerError, "issuer does not support passkey")

			return
		}

		respBody, statusCode, err = passkeyIssuer.PasskeyToken(r.Context(), r, body)
	} else {
		// remote auth middleware over HTTP
		respBody, statusCode, err = m.RemotePasskeyToken(r, provider.Oauth2.PasskeyURL, body)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())

		return
	}

	// a finish response carries tokens; persist them in the session
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if statusCode >= 200 && statusCode <= 299 && json.Unmarshal(respBody, &token) == nil && token.AccessToken != "" {
		if err := sessionM.SetToken(w, r, respBody, providerName); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())

			return
		}

		httputil.NoContent(w, http.StatusNoContent)

		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(respBody)
}
