package auth

import (
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

const (
	grantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"

	tokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token"
)

// tokenExchangeGrant implements RFC 8693 token exchange (impersonation).
// Only confidential clients may exchange tokens.
func (m *Auth) tokenExchangeGrant(w http.ResponseWriter, r *http.Request, req AccessTokenRequest, clientID, clientSecret string) {
	if m.cache.Snapshot().TokenExchange.Disabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unsupported_grant_type",
			ErrorDescription: "token exchange is disabled",
			code:             http.StatusBadRequest,
		})

		return
	}

	accessClient, err := m.GetAccessClient(clientID, clientSecret)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	if req.SubjectToken == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "subject_token is required",
			code:             http.StatusBadRequest,
		})

		return
	}

	if req.SubjectTokenType != "" && req.SubjectTokenType != tokenTypeAccessToken {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "subject_token_type not supported",
			code:             http.StatusBadRequest,
		})

		return
	}

	signer, err := m.jwtRuntime(r.Context())
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	claims := jwt.MapClaims{}
	if _, err := signer.JWT.Parse(req.SubjectToken, &claims); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	// refresh tokens are not exchangeable
	if typ, _ := claims["typ"].(string); typ == "Refresh" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "subject_token must be an access token",
			code:             http.StatusBadRequest,
		})

		return
	}

	userID, _ := claims["sub"].(string)
	user, err := m.cache.GetUser(data.GetUserRequest{ID: userID, AddScopeRoles: true})
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "user not found",
			code:             http.StatusUnauthorized,
		})

		return
	}

	// requested scope falls back to the subject token's scope
	scope := splitFields(req.Scope)
	if len(scope) == 0 {
		subjectScope, _ := claims["scope"].(string)
		scope = splitFields(subjectScope)
	}

	m.writeTokenExt(w, r, user, clientID, scope, accessClient.Scope, tokenTypeAccessToken, "")
}
