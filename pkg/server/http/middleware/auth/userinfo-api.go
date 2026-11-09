package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// APIUserInfo returns claims for a bearer access token. OIDC Core §5.3.1
// requires GET and POST; the token comes from the Authorization header or,
// for POST, from a form-encoded access_token body parameter (RFC 6750 §2.2).
func (m *Auth) APIUserInfo(w http.ResponseWriter, r *http.Request) {
	authorizationHeader := r.Header.Get("Authorization")
	tokenHeader, scheme := session.AuthorizationToken(authorizationHeader)
	if tokenHeader == "" && authorizationHeader == "" && r.Method == http.MethodPost &&
		strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if err := r.ParseForm(); err == nil {
			tokenHeader = r.PostForm.Get("access_token")
		}
	}
	if tokenHeader == "" {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_request"`)
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "access token not found",
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
	if err := m.parseOwnToken(signer, tokenHeader, &claims); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}
	if typ, _ := claims["typ"].(string); typ != "Bearer" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "access token type must be Bearer",
			code:             http.StatusUnauthorized,
		})

		return
	}

	if err := m.checkTokenBinding(r, claims, scheme, tokenHeader); err != nil {
		w.Header().Set("WWW-Authenticate", `DPoP error="invalid_token", algs="`+strings.Join(dpopAlgorithms, " ")+`"`)
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

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
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "token revoked",
			code:             http.StatusUnauthorized,
		})

		return
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "user not found",
			code:             http.StatusUnauthorized,
		})

		return
	}

	user, err := m.cache.GetUser(data.GetUserRequest{ID: sub})
	if err != nil || user.Disabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "user not found",
			code:             http.StatusUnauthorized,
		})

		return
	}

	sn := m.cache.Snapshot()

	grantedScopes := map[string]struct{}{}
	if scope, _ := claims["scope"].(string); scope != "" {
		for _, s := range strings.Fields(scope) {
			grantedScopes[s] = struct{}{}
		}
	}

	claimsRet := map[string]any{"sub": user.ID}
	for k, v := range userStandardClaims(user, grantedScopes, sn.Token.OIDCStandardClaims) {
		claimsRet[k] = v
	}

	// Mirror the roles the presented access token carries. Reading them back
	// off the token (instead of recomputing from the user) keeps userinfo
	// bound to the scopes that were actually granted at issue time, and keeps
	// a revoked/narrowed scope from silently widening through this endpoint.
	azp, _ := claims["azp"].(string)
	rolesClaim := rolesClaimPath(sn, azp)

	if rolesList := claimValues(claims, rolesClaim); len(rolesList) > 0 {
		setClaimByPath(claimsRet, rolesClaim, rolesList)
	}

	// apply custom userinfo templates selected by the {custom} path segment.
	if customName := r.PathValue("custom"); customName != "" {
		if !sn.CustomInfo.Disabled {
			if set, ok := sn.CustomInfo.Sets[customName]; ok {
				// render against a copy of the base claims so iteration order
				// never affects the result; templates may add or overwrite claims.
				base := make(map[string]any, len(claimsRet))
				for k, v := range claimsRet {
					base[k] = v
				}

				tmplData := map[string]any{"claims": base, "user": user}
				for k, tmpl := range set.Claims {
					result, err := render.ExecuteWithData(tmpl, tmplData)
					if err != nil {
						slog.Error("failed to render custom info", "error", err, "custom", customName, "key", k)

						continue
					}

					// an empty render result removes the claim from the response;
					// this lets a set drop default claims (use {{- -}} to trim).
					if len(result) == 0 {
						delete(claimsRet, k)

						continue
					}

					claimsRet[k] = string(result)
				}
			}
		}
	}

	httputil.JSON(w, http.StatusOK, claimsRet)
}
