package auth

import (
	"net/http"
	"slices"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
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
	if err := m.parseOwnToken(signer, req.SubjectToken, &claims); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	if typ, _ := claims["typ"].(string); typ != "Bearer" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "subject_token must be an access token",
			code:             http.StatusBadRequest,
		})

		return
	}

	// revoked subject tokens must not be exchangeable
	revoked, err := m.claimsRevoked(r.Context(), claims)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}
	if revoked {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "token revoked",
			code:             http.StatusUnauthorized,
		})

		return
	}

	// RFC 8693 lets the exchanging client differ from the one the subject
	// token was issued to (a downstream service acting for the user); the
	// allow-list narrows that down when configured
	if subjectClient, _ := claims["azp"].(string); subjectClient != clientID && !m.cache.Snapshot().TokenExchange.clientAllowed(clientID) {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unauthorized_client",
			ErrorDescription: "client is not allowed to exchange this token",
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

	// an exchange may narrow the subject token's scope, never widen it
	subjectScope, _ := claims["scope"].(string)
	scope, ok := exchangeScope(splitFields(req.Scope), splitFields(subjectScope))
	if !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_scope",
			ErrorDescription: "requested scope exceeds the subject token's scope",
			code:             http.StatusBadRequest,
		})

		return
	}

	// RFC 8707: an explicit resource wins, otherwise the subject token's
	// resource audiences carry over; either way the exchanging client must
	// be allowed to target them
	resources := audienceResources(claims["aud"])
	if req.Resource != "" {
		if err := validateResource(req.Resource); err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_target",
				ErrorDescription: err.Error(),
				code:             http.StatusBadRequest,
			})

			return
		}

		resources = []string{req.Resource}
	}

	if !resourcesAllowed(resources, accessClient.Resources) {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_target",
			ErrorDescription: "requested resource not allowed",
			code:             http.StatusBadRequest,
		})

		return
	}

	// stay inside the subject token's session: same sid (so revoking the
	// session revokes the exchanged token), same auth_time and lifetime cap,
	// and no refresh token that could outlive it
	sessionExpires := claimInt64(claims["session_exp"])
	if sessionExpires == 0 {
		sessionExpires = claimInt64(claims["exp"])
	}
	sid, _ := claims["sid"].(string)

	m.writeTokenWithOptions(w, r, user, clientID, scope, nil, tokenIssueOptions{
		IssuedTokenType: tokenTypeAccessToken,
		Resources:       resources,
		SID:             sid,
		AuthTime:        claimInt64(claims["auth_time"]),
		SessionExpires:  sessionExpires,
		NoRefresh:       true,
	})
}

// exchangeScope returns the scope for an exchanged token: the subject's
// scope when none is requested, otherwise the requested scope provided it is
// a subset of the subject's.
func exchangeScope(requested, subject []string) ([]string, bool) {
	if len(requested) == 0 {
		return subject, true
	}

	for _, s := range requested {
		if !slices.Contains(subject, s) {
			return nil, false
		}
	}

	return requested, true
}
