package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// revokedToken is the payload stored for a revoked token jti or session sid.
type revokedToken struct {
	Subject  string `json:"sub,omitempty"`
	ClientID string `json:"azp,omitempty"`
	Type     string `json:"typ,omitempty"`
}

// isTokenRevoked reports whether the jti is on the revocation list.
func (m *Auth) isTokenRevoked(ctx context.Context, jti string) bool {
	revoked, _ := m.revocationStatus(ctx, flowKindRevoked, jti)

	return revoked
}

func (m *Auth) revocationStatus(ctx context.Context, kind, id string) (bool, error) {
	if id == "" {
		return false, nil
	}

	var payload revokedToken
	err := m.store.GetFlowCode(ctx, kind, id, &payload)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, data.ErrNotFound) {
		return false, nil
	}

	return false, err
}

func (m *Auth) claimsRevoked(ctx context.Context, claims jwt.MapClaims) (bool, error) {
	jti, _ := claims["jti"].(string)
	sid, _ := claims["sid"].(string)
	revoked, err := m.revocationStatus(ctx, flowKindRevoked, jti)
	if err != nil || revoked {
		return revoked, err
	}

	return m.revocationStatus(ctx, flowKindRevokedSession, sid)
}

func claimInt64(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()

		return n
	default:
		return 0
	}
}

// TokenRevoked exposes the revocation list to other middlewares (e.g. the
// oauth2resource middleware) through the issuer registry.
func (m *Auth) TokenRevoked(ctx context.Context, jti string) bool {
	return m.isTokenRevoked(ctx, jti)
}

// TokenClaimsRevoked checks both a token and its session family. Resource
// middleware uses the error to fail closed when PostgreSQL is unavailable.
func (m *Auth) TokenClaimsRevoked(ctx context.Context, jti, sid string) (bool, error) {
	revoked, err := m.revocationStatus(ctx, flowKindRevoked, jti)
	if err != nil || revoked {
		return revoked, err
	}

	return m.revocationStatus(ctx, flowKindRevokedSession, sid)
}

// revokeRawToken parses a token issued by this middleware. Access tokens put
// their jti on the denylist; refresh tokens revoke the complete sid family.
// Invalid or already expired tokens are a no-op (RFC 7009 semantics).
func (m *Auth) revokeRawToken(ctx context.Context, token string) error {
	signer, err := m.jwtRuntime(ctx)
	if err != nil {
		return err
	}

	claims := jwt.MapClaims{}
	if _, err := signer.JWT.Parse(token, &claims); err != nil {
		return nil //nolint:nilerr // foreign/expired tokens need no revocation
	}

	jti, _ := claims["jti"].(string)
	sid, _ := claims["sid"].(string)
	typ, _ := claims["typ"].(string)
	exp := claimInt64(claims["exp"])
	if jti == "" || exp <= 0 {
		return nil
	}

	kind := flowKindRevoked
	id := jti
	if typ == "Refresh" {
		if sid == "" {
			sid = jti
		}
		kind = flowKindRevokedSession
		id = sid
		if sessionExp := claimInt64(claims["session_exp"]); sessionExp > exp {
			exp = sessionExp
		}
	}

	ttl := time.Until(time.Unix(exp, 0))
	if ttl <= 0 {
		return nil
	}

	sub, _ := claims["sub"].(string)
	azp, _ := claims["azp"].(string)

	payload := revokedToken{
		Subject:  sub,
		ClientID: azp,
		Type:     typ,
	}
	_, err = m.store.CreateFlowCodeOnce(ctx, kind, id, payload, ttl)
	if err != nil {
		return err
	}

	// Keep rolling upgrades safe: older replicas only know the per-JTI list.
	if kind == flowKindRevokedSession {
		_, err = m.store.CreateFlowCodeOnce(ctx, flowKindRevoked, jti, payload, ttl)
	}

	return err
}

// RevokeToken implements session.InfRevoker so the login middleware can
// revoke session tokens on logout.
func (m *Auth) RevokeToken(ctx context.Context, token string) error {
	return m.revokeRawToken(ctx, token)
}

// revokeRequest is the RFC 7009 form payload; client credentials may come
// from the body or basic auth.
type revokeRequest struct {
	Token         string `form:"token"           json:"token"`
	TokenTypeHint string `form:"token_type_hint" json:"token_type_hint"`
	ClientID      string `form:"client_id"       json:"client_id"`
	ClientSecret  string `form:"client_secret"   json:"client_secret"`
}

// APIRevoke implements RFC 7009 token revocation. Tokens are stateless JWTs,
// so revocation stores the jti on a denylist until the token expires; the
// refresh grant, token exchange and introspection honor that list.
func (m *Auth) APIRevoke(w http.ResponseWriter, r *http.Request) {
	req := revokeRequest{}
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	clientID, clientSecret := req.ClientID, req.ClientSecret
	if id, secret, ok := r.BasicAuth(); ok {
		clientID, clientSecret = m.basicClientCredentials(id, secret)
	}

	if clientID == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: "client credentials not provided",
			code:             http.StatusUnauthorized,
		})

		return
	}

	if _, err := m.resolveClient(clientID, clientSecret); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	if req.Token == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "token is required",
			code:             http.StatusBadRequest,
		})

		return
	}

	// per RFC 7009 §2.2 an invalid or already expired token still returns 200
	if err := m.revokeRawToken(r.Context(), req.Token); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

// APIIntrospect implements RFC 7662 token introspection.
func (m *Auth) APIIntrospect(w http.ResponseWriter, r *http.Request) {
	req := revokeRequest{}
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	clientID, clientSecret := req.ClientID, req.ClientSecret
	if id, secret, ok := r.BasicAuth(); ok {
		clientID, clientSecret = m.basicClientCredentials(id, secret)
	}

	if clientID == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: "client credentials not provided",
			code:             http.StatusUnauthorized,
		})

		return
	}

	if _, err := m.resolveClient(clientID, clientSecret); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	w.Header().Set("Cache-Control", "no-store")

	inactive := map[string]any{"active": false}

	if req.Token == "" {
		httputil.JSON(w, http.StatusOK, inactive)

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
	if _, err := signer.JWT.Parse(req.Token, &claims,
		jwt.WithIssuer(m.issuerURL(r)), jwt.WithAudience("turna-auth")); err != nil {
		httputil.JSON(w, http.StatusOK, inactive)

		return
	}

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
		httputil.JSON(w, http.StatusOK, inactive)

		return
	}

	response := map[string]any{
		"active":     true,
		"token_type": "Bearer",
		"iss":        m.issuerURL(r),
	}

	for _, key := range []string{"scope", "sub", "aud", "exp", "iat", "jti", "azp", "typ", "preferred_username", "email"} {
		if v, ok := claims[key]; ok {
			response[key] = v
		}
	}

	if azp, ok := claims["azp"].(string); ok {
		response["client_id"] = azp
	}

	if username, ok := claims["preferred_username"].(string); ok {
		response["username"] = username
	}

	httputil.JSON(w, http.StatusOK, response)
}
