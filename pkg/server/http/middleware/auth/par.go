package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

const (
	parRequestURIPrefix = "urn:ietf:params:oauth:request_uri:"
	// parLifetime is how long a pushed request stays redeemable (RFC 9126
	// §2.2 recommends a short lifetime, between 5 and 600 seconds).
	parLifetime = 90 * time.Second
)

// pushedRequest is a stored pushed authorization request.
type pushedRequest struct {
	ClientID string `json:"client_id"`
	Query    string `json:"query"`
}

// APIPushedAuthorizationRequest implements RFC 9126: the client posts the
// authorization request parameters with its credentials and gets a one-time
// request_uri for /oauth2/authorize.
func (m *Auth) APIPushedAuthorizationRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_request", ErrorDescription: "invalid form body", code: http.StatusBadRequest})

		return
	}

	form := r.PostForm
	clientID, clientSecret := m.clientCredentials(r, AccessTokenRequest{
		ClientID:     form.Get("client_id"),
		ClientSecret: form.Get("client_secret"),
	})
	if clientID == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_client", ErrorDescription: "client_id is required", code: http.StatusUnauthorized})

		return
	}

	client, err := m.authorizationRequestClient(r.Context(), clientID)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_client", ErrorDescription: "client not found", code: http.StatusUnauthorized})

		return
	}

	// confidential clients authenticate; public clients rely on PKCE
	if client.ClientSecret != "" && !clientSecretMatches(client.ClientSecret, clientSecret) {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_client", ErrorDescription: "client authentication failed", code: http.StatusUnauthorized})

		return
	}

	// RFC 9126 §2.1: a client_id in the body must match the authenticated one
	if bodyID := form.Get("client_id"); bodyID != "" && bodyID != clientID {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_request", ErrorDescription: "client_id mismatch", code: http.StatusBadRequest})

		return
	}

	if form.Get("request_uri") != "" {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_request", ErrorDescription: "request_uri is not allowed in a pushed request", code: http.StatusBadRequest})

		return
	}
	if form.Get("request") != "" {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "request_not_supported", ErrorDescription: "request objects are not supported", code: http.StatusBadRequest})

		return
	}

	// validate early what /authorize would reject anyway (RFC 9126 §2.3)
	if !client.redirectURIAllowedForClient(form.Get("redirect_uri")) {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_request", ErrorDescription: "redirect_uri not allowed", code: http.StatusBadRequest})

		return
	}

	params := url.Values{}
	for k, v := range form {
		switch k {
		case "client_secret", "client_assertion", "client_assertion_type":
			continue
		}
		params[k] = v
	}
	params.Set("client_id", clientID)

	id, err := randomHex(32)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "server_error", ErrorDescription: err.Error(), code: http.StatusInternalServerError})

		return
	}

	if err := m.store.CreateFlowCode(r.Context(), flowKindPushedRequest, id, pushedRequest{ClientID: clientID, Query: params.Encode()}, parLifetime); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "server_error", ErrorDescription: err.Error(), code: http.StatusInternalServerError})

		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httputil.JSON(w, http.StatusCreated, map[string]any{
		"request_uri": parRequestURIPrefix + id,
		"expires_in":  int(parLifetime.Seconds()),
	})
}

var errPushedRequest = errors.New("request_uri is invalid, expired or already used")

// loadPushedRequest replaces the query of an authorization request that
// references a pushed request with the stored parameters. The pushed
// request is single use and bound to the client that pushed it.
func (m *Auth) loadPushedRequest(r *http.Request) error {
	query := r.URL.Query()
	requestURI := query.Get("request_uri")
	id, ok := strings.CutPrefix(requestURI, parRequestURIPrefix)
	if !ok || id == "" {
		return errPushedRequest
	}

	var pushed pushedRequest
	if err := m.store.TakeFlowCode(r.Context(), flowKindPushedRequest, id, &pushed); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return errPushedRequest
		}

		return err
	}

	if pushed.ClientID != query.Get("client_id") {
		return errPushedRequest
	}

	r.URL.RawQuery = pushed.Query

	return nil
}
