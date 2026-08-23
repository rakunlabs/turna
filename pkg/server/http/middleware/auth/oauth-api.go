package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"golang.org/x/crypto/bcrypt"
)

type AccessTokenRequest struct {
	GrantType    string `form:"grant_type"    json:"grant_type"`
	Code         string `form:"code"          json:"code"`
	RedirectURI  string `form:"redirect_uri"  json:"redirect_uri"`
	ClientID     string `form:"client_id"     json:"client_id"`
	ClientSecret string `form:"client_secret" json:"client_secret"`
	RefreshToken string `form:"refresh_token" json:"refresh_token"`
	Username     string `form:"username"      json:"username"`
	Password     string `form:"password"      json:"password"`
	RememberMe   bool   `form:"remember_me"   json:"remember_me"`
	Scope        string `form:"scope"         json:"scope"`
	// TOTP second factor for the password grant.
	TOTP string `form:"totp" json:"totp"`
	// DeviceCode for the RFC 8628 device flow.
	DeviceCode string `form:"device_code" json:"device_code"`
	// SubjectToken/SubjectTokenType for RFC 8693 token exchange.
	SubjectToken     string `form:"subject_token"      json:"subject_token"`
	SubjectTokenType string `form:"subject_token_type" json:"subject_token_type"`
	// CodeVerifier for PKCE (RFC 7636) on the authorization_code grant.
	CodeVerifier string `form:"code_verifier" json:"code_verifier"`
	// Resource is an RFC 8707 resource indicator; it lands in the access
	// token audience so resource servers can check it.
	Resource string `form:"resource" json:"resource"`
}

type AccessTokenResponse struct {
	TokenType             string `json:"token_type"`
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	RefreshTokenExpiresIn int64  `json:"refresh_expires_in,omitempty"`
	Scope                 string `json:"scope,omitempty"`
	// IDToken is issued when the granted scope contains "openid".
	IDToken string `json:"id_token,omitempty"`
	// IssuedTokenType is set for RFC 8693 token exchange responses.
	IssuedTokenType string `json:"issued_token_type,omitempty"`
}

type AccessTokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`

	code int `json:"-"`
}

func (e AccessTokenErrorResponse) GetCode() int {
	return e.code
}

type JWK struct {
	KID string   `json:"kid"`
	KTY string   `json:"kty"`
	ALG string   `json:"alg"`
	Use string   `json:"use"`
	N   string   `json:"n"`
	E   string   `json:"e"`
	X5C []string `json:"x5c,omitempty"`
}

type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

func splitFields(v string) []string {
	if v == "" {
		return nil
	}

	return strings.Fields(v)
}

func compareBcryptBase64(hash, password string) error {
	hashBytes, err := base64.StdEncoding.DecodeString(hash)
	if err != nil {
		return err
	}

	return bcrypt.CompareHashAndPassword(hashBytes, []byte(password))
}

func clientCredentials(r *http.Request, req AccessTokenRequest) (string, string) {
	if clientID, clientSecret, ok := r.BasicAuth(); ok {
		return clientID, clientSecret
	}

	return req.ClientID, req.ClientSecret
}

// tokenAudience builds the aud claim: the fixed local audience plus any
// RFC 8707 resource indicators granted with the token.
func tokenAudience(resources []string) any {
	if len(resources) == 0 {
		return "turna-auth"
	}

	aud := make([]string, 0, len(resources)+1)
	aud = append(aud, "turna-auth")
	aud = append(aud, resources...)

	return aud
}

// audienceResources extracts the RFC 8707 resources back out of an aud claim.
func audienceResources(aud any) []string {
	list, ok := aud.([]any)
	if !ok {
		return nil
	}

	resources := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" && s != "turna-auth" {
			resources = append(resources, s)
		}
	}

	return resources
}

// writeToken issues access+refresh tokens for the user.
func (m *Auth) writeToken(w http.ResponseWriter, r *http.Request, user *data.UserExtended, clientID string, scope, defScope []string) {
	m.writeTokenExt(w, r, user, clientID, scope, defScope, "", "", nil)
}

type tokenIssueOptions struct {
	IssuedTokenType string
	Nonce           string
	Resources       []string
	RememberMe      bool
	SID             string
	AuthTime        int64
	SessionExpires  int64
	RefreshToken    string
	RefreshExpires  int64
}

// writeTokenExt issues access+refresh tokens for the user, optionally
// tagging the response with an RFC 8693 issued_token_type, embedding
// the OIDC nonce in the id_token and adding RFC 8707 resource audiences.
func (m *Auth) writeTokenExt(w http.ResponseWriter, r *http.Request, user *data.UserExtended, clientID string, scope, defScope []string, issuedTokenType, nonce string, resources []string) {
	m.writeTokenWithOptions(w, r, user, clientID, scope, defScope, tokenIssueOptions{
		IssuedTokenType: issuedTokenType,
		Nonce:           nonce,
		Resources:       resources,
	})
}

func (m *Auth) writeTokenWithOptions(w http.ResponseWriter, r *http.Request, user *data.UserExtended, clientID string, scope, defScope []string, options tokenIssueOptions) {
	ctx := r.Context()
	signer, err := m.jwtRuntime(ctx)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	sn := m.cache.Snapshot()
	tokenCfg := sn.Token
	issuer := m.issuerURL(r)
	now := time.Now()

	if options.SID == "" {
		options.SID = ulid.Make().String()
	}
	if options.AuthTime == 0 {
		options.AuthTime = now.Unix()
	}
	if options.SessionExpires == 0 {
		lifetime := tokenCfg.GetRefreshLifetime()
		if options.RememberMe {
			lifetime = tokenCfg.GetRefreshAbsoluteLifetime()
		}
		options.SessionExpires = now.Add(lifetime).Unix()
	}
	if options.SessionExpires <= now.Unix() {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "session maximum lifetime exceeded",
			code:             http.StatusUnauthorized,
		})

		return
	}

	accessExpires := min(now.Add(tokenCfg.GetTokenLifetime()).Unix(), options.SessionExpires)

	claimsAccess := map[string]any{
		"iss":                issuer,
		"aud":                tokenAudience(options.Resources),
		"jti":                ulid.Make().String(),
		"sub":                user.ID,
		"azp":                clientID,
		"sid":                options.SID,
		"auth_time":          options.AuthTime,
		"session_exp":        options.SessionExpires,
		"remember_me":        options.RememberMe,
		"name":               user.Details["name"],
		"preferred_username": user.Details["name"],
	}

	if v, ok := user.Details["email"]; ok {
		claimsAccess["email"] = v
	}
	if v, ok := user.Details["uid"]; ok {
		claimsAccess["preferred_username"] = v
	}
	if v, ok := user.Details["given_name"]; ok {
		claimsAccess["given_name"] = v
	}
	if v, ok := user.Details["family_name"]; ok {
		claimsAccess["family_name"] = v
	}

	// scope and roles
	scopeMap := make(map[string]struct{})
	for _, s := range defScope {
		if s != "" {
			scopeMap[s] = struct{}{}
		}
	}
	for _, s := range scope {
		if s != "" {
			scopeMap[s] = struct{}{}
		}
	}

	roles := make(map[string]struct{})
	scopeList := make([]string, 0, len(scopeMap))
	for s := range scopeMap {
		if scopeRoles, ok := user.Scope[s]; ok {
			for _, role := range scopeRoles {
				roles[role] = struct{}{}
			}
		}

		scopeList = append(scopeList, s)
	}

	if len(scopeList) > 0 {
		claimsAccess["scope"] = strings.Join(scopeList, " ")
	}

	rolesList := make([]string, 0, len(roles))
	for role := range roles {
		rolesList = append(rolesList, role)
	}

	if len(rolesList) > 0 {
		rolesClaim := tokenCfg.GetRolesClaim()
		if client, ok := sn.OAuthClients[clientID]; ok && client.RolesClaim != "" {
			rolesClaim = client.RolesClaim
		}

		setClaimByPath(claimsAccess, rolesClaim, rolesList)
	}

	accessToken, err := signer.JWT.Generate(claimsAccess, accessExpires)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	refreshToken := options.RefreshToken
	refreshExpires := options.RefreshExpires
	if refreshToken == "" || options.RememberMe {
		refreshExpires = min(now.Add(tokenCfg.GetRefreshLifetime()).Unix(), options.SessionExpires)
		claimsRefresh := map[string]any{
			"iss":         issuer,
			"aud":         tokenAudience(options.Resources),
			"jti":         ulid.Make().String(),
			"sub":         user.ID,
			"azp":         clientID,
			"typ":         "Refresh",
			"sid":         options.SID,
			"auth_time":   options.AuthTime,
			"session_exp": options.SessionExpires,
			"remember_me": options.RememberMe,
		}

		if len(scopeList) > 0 {
			claimsRefresh["scope"] = strings.Join(scopeList, " ")
		}

		refreshToken, err = signer.JWT.Generate(claimsRefresh, refreshExpires)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}
	}

	// id_token for OIDC clients
	idToken := ""
	if _, ok := scopeMap["openid"]; ok {
		claimsID := map[string]any{
			"iss":                m.issuerURL(r),
			"aud":                clientID,
			"azp":                clientID,
			"sub":                user.ID,
			"sid":                options.SID,
			"auth_time":          options.AuthTime,
			"iat":                time.Now().Unix(),
			"name":               user.Details["name"],
			"preferred_username": claimsAccess["preferred_username"],
		}

		if v, ok := user.Details["email"]; ok {
			claimsID["email"] = v
		}
		if v, ok := user.Details["given_name"]; ok {
			claimsID["given_name"] = v
		}
		if v, ok := user.Details["family_name"]; ok {
			claimsID["family_name"] = v
		}
		if options.Nonce != "" {
			claimsID["nonce"] = options.Nonce
		}

		idToken, err = signer.JWT.Generate(claimsID, accessExpires)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	httputil.JSON(w, http.StatusOK, AccessTokenResponse{
		TokenType:             "Bearer",
		AccessToken:           accessToken,
		ExpiresIn:             max(accessExpires-now.Unix(), 0),
		RefreshToken:          refreshToken,
		RefreshTokenExpiresIn: max(refreshExpires-now.Unix(), 0),
		Scope:                 strings.Join(scopeList, " "),
		IDToken:               idToken,
		IssuedTokenType:       options.IssuedTokenType,
	})
}

// APIToken implements the token endpoint.
func (m *Auth) APIToken(w http.ResponseWriter, r *http.Request) {
	accessTokenRequest := AccessTokenRequest{}
	if err := httputil.Decode(r, &accessTokenRequest); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	clientID, clientSecret := clientCredentials(r, accessTokenRequest)
	if clientID == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: "client credentials not provided",
			code:             http.StatusBadRequest,
		})

		return
	}

	// RFC 8707 resource indicator applies to the direct grants; the
	// authorization_code grant resolves it against the stored code below.
	var reqResources []string
	if accessTokenRequest.Resource != "" {
		if err := validateResource(accessTokenRequest.Resource); err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_target",
				ErrorDescription: err.Error(),
				code:             http.StatusBadRequest,
			})

			return
		}

		reqResources = []string{accessTokenRequest.Resource}
	}

	switch accessTokenRequest.GrantType {
	case "client_credentials":
		// certificate based client authentication (RFC 8705 style) when no
		// secret is provided and the "mtls" setting is enabled
		if clientSecret == "" {
			user, err := m.mtlsAuthenticate(r, clientID)
			if err != nil {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: err.Error(),
					code:             http.StatusUnauthorized,
				})

				return
			}

			scope, _ := user.Details["scope"].(string)

			m.writeTokenExt(w, r, user, clientID, nil, splitFields(scope), "", "", reqResources)

			return
		}

		user, err := m.cache.GetUser(data.GetUserRequest{
			Alias:          clientID,
			ServiceAccount: &data.True,
			AddScopeRoles:  true,
		})
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		if secret, _ := user.Details["secret"].(string); secret == "" || secret != clientSecret {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "secret not match",
				code:             http.StatusUnauthorized,
			})

			return
		}

		scope, _ := user.Details["scope"].(string)

		m.writeTokenExt(w, r, user, clientID, nil, splitFields(scope), "", "", reqResources)

		return
	case "password":
		passwordCfg := m.cache.Snapshot().Password
		if passwordCfg.Disabled {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "unsupported_grant_type",
				ErrorDescription: "password login is disabled",
				code:             http.StatusBadRequest,
			})

			return
		}

		accessTokenRequest.Username = strings.TrimSpace(accessTokenRequest.Username)
		if accessTokenRequest.Username == "" || accessTokenRequest.Password == "" {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: "username and password are required",
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

		userReq := data.GetUserRequest{
			Alias:         accessTokenRequest.Username,
			AddScopeRoles: true,
		}

		var user *data.UserExtended
		if passwordCfg.LdapRegisterDisabled {
			// only already-known users; no on-demand LDAP sync
			user, err = m.cache.GetUser(userReq)
		} else {
			user, err = m.GetOrCreateUser(r.Context(), userReq)
		}
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		if user.Local {
			if passwordCfg.LocalDisabled {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: "local password login is disabled",
					code:             http.StatusUnauthorized,
				})

				return
			}

			password, _ := user.Details["password"].(string)
			if password == "" || compareBcryptBase64(password, accessTokenRequest.Password) != nil {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: "password not match",
					code:             http.StatusUnauthorized,
				})

				return
			}
		} else {
			if passwordCfg.LdapDisabled {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: "ldap password login is disabled",
					code:             http.StatusUnauthorized,
				})

				return
			}

			ok, err := m.LdapCheckPassword(accessTokenRequest.Username, accessTokenRequest.Password)
			if err != nil {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "server_error",
					ErrorDescription: err.Error(),
					code:             http.StatusInternalServerError,
				})

				return
			}

			if !ok {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: "password not match",
					code:             http.StatusUnauthorized,
				})

				return
			}
		}

		// second factor: users with a confirmed totp secret must send one
		if totpCfg := m.cache.Snapshot().TOTP; !totpCfg.Disabled {
			if secret, confirmed, err := m.store.GetTOTPSecret(r.Context(), user.ID); err == nil && confirmed {
				if accessTokenRequest.TOTP == "" {
					httputil.HandleError(w, AccessTokenErrorResponse{
						Error:            "mfa_required",
						ErrorDescription: "totp code required",
						code:             http.StatusUnauthorized,
					})

					return
				}

				// a single-use recovery code is accepted in place of the totp code
				if !validateTOTP(secret, accessTokenRequest.TOTP, totpCfg.GetSkew(), time.Now()) &&
					!m.store.ConsumeTOTPRecoveryCode(r.Context(), user.ID, accessTokenRequest.TOTP) {
					httputil.HandleError(w, AccessTokenErrorResponse{
						Error:            "invalid_grant",
						ErrorDescription: "totp code invalid",
						code:             http.StatusUnauthorized,
					})

					return
				}
			}
		}

		if !resourcesAllowed(reqResources, accessClient.Resources) {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_target",
				ErrorDescription: "requested resource not allowed",
				code:             http.StatusBadRequest,
			})

			return
		}

		m.writeTokenWithOptions(w, r, user, clientID, splitFields(accessTokenRequest.Scope), accessClient.Scope, tokenIssueOptions{
			Resources:  reqResources,
			RememberMe: accessTokenRequest.RememberMe,
		})

		return
	case "refresh_token":
		if _, err := m.authorizationClient(r.Context(), clientID, clientSecret); err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_client",
				ErrorDescription: err.Error(),
				code:             http.StatusUnauthorized,
			})

			return
		}

		if accessTokenRequest.RefreshToken == "" {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: "refresh token not provided",
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
		if _, err := signer.JWT.Parse(accessTokenRequest.RefreshToken, &claims,
			jwt.WithIssuer(m.issuerURL(r)), jwt.WithAudience("turna-auth")); err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: err.Error(),
				code:             http.StatusUnauthorized,
			})

			return
		}

		if claims["typ"] != "Refresh" {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "invalid token type",
				code:             http.StatusUnauthorized,
			})

			return
		}

		if azp, _ := claims["azp"].(string); azp == "" || azp != clientID {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "refresh token was issued to another client",
				code:             http.StatusUnauthorized,
			})

			return
		}

		jti, _ := claims["jti"].(string)
		exp, err := claims.GetExpirationTime()
		if err != nil || exp == nil || jti == "" {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "refresh token lacks session claims",
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
				Error:            "invalid_grant",
				ErrorDescription: "refresh token revoked",
				code:             http.StatusUnauthorized,
			})

			return
		}

		rememberMe, _ := claims["remember_me"].(bool)
		sid, _ := claims["sid"].(string)
		if sid == "" {
			sid = jti
		}
		authTime := claimInt64(claims["auth_time"])
		if authTime == 0 {
			authTime = claimInt64(claims["iat"])
		}
		if authTime == 0 {
			authTime = time.Now().Unix()
		}
		sessionExpires := claimInt64(claims["session_exp"])
		if sessionExpires == 0 {
			if rememberMe {
				sessionExpires = time.Unix(authTime, 0).Add(m.cache.Snapshot().Token.GetRefreshAbsoluteLifetime()).Unix()
			} else {
				sessionExpires = exp.Time.Unix()
			}
		}
		if sessionExpires <= time.Now().Unix() {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "session maximum lifetime exceeded",
				code:             http.StatusUnauthorized,
			})

			return
		}

		subject, _ := claims["sub"].(string)
		user, err := m.cache.GetUser(data.GetUserRequest{ID: subject, AddScopeRoles: true})
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		scope, _ := claims["scope"].(string)

		// resource audiences granted at login survive the refresh
		m.writeTokenWithOptions(w, r, user, clientID, splitFields(scope), nil, tokenIssueOptions{
			Resources:      audienceResources(claims["aud"]),
			RememberMe:     rememberMe,
			SID:            sid,
			AuthTime:       authTime,
			SessionExpires: sessionExpires,
			RefreshToken:   accessTokenRequest.RefreshToken,
			RefreshExpires: exp.Time.Unix(),
		})

		return
	case "authorization_code":
		// PKCE clients may be public (no secret); the verifier check below
		// is the proof of possession
		var accessClient *AccessClient
		var err error
		if accessTokenRequest.CodeVerifier != "" {
			accessClient, err = m.authorizationClient(r.Context(), clientID, clientSecret)
		} else {
			accessClient, err = m.GetAccessClient(clientID, clientSecret)
		}
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_client",
				ErrorDescription: err.Error(),
				code:             http.StatusUnauthorized,
			})

			return
		}

		if accessTokenRequest.Code == "" {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: "code not found",
				code:             http.StatusBadRequest,
			})

			return
		}

		codeStore, err := m.codeStoreRuntime(r.Context())
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		codeRaw, ok, err := codeStore.Code.Get(r.Context(), "code_"+accessTokenRequest.Code)
		if err != nil || !ok {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "code not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		codeValue, err := oauth2store.Decode[oauth2store.Code](codeRaw)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		_ = codeStore.Code.Delete(r.Context(), "code_"+accessTokenRequest.Code)

		// codes issued by the local authorize endpoint are bound to the
		// requesting client and redirect target (RFC 6749 §4.1.3)
		if codeValue.ClientID != "" && codeValue.ClientID != clientID {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "code was issued to another client",
				code:             http.StatusUnauthorized,
			})

			return
		}

		if codeValue.RedirectURI != "" && codeValue.RedirectURI != accessTokenRequest.RedirectURI {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "redirect_uri not match",
				code:             http.StatusUnauthorized,
			})

			return
		}

		// PKCE: a stored challenge requires a matching verifier and a sent
		// verifier requires a stored challenge (no bypass for public clients)
		if codeValue.CodeChallenge != "" || accessTokenRequest.CodeVerifier != "" {
			if codeValue.CodeChallenge == "" || accessTokenRequest.CodeVerifier == "" ||
				!verifyPKCE(codeValue.CodeChallenge, codeValue.CodeChallengeMethod, accessTokenRequest.CodeVerifier) {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: "code verifier not match",
					code:             http.StatusUnauthorized,
				})

				return
			}
		}

		// RFC 8707: a resource sent at the token endpoint must stay within
		// what the authorization request granted (or the client allows)
		resources := codeValue.Resources
		if accessTokenRequest.Resource != "" {
			if err := validateResource(accessTokenRequest.Resource); err != nil {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_target",
					ErrorDescription: err.Error(),
					code:             http.StatusBadRequest,
				})

				return
			}

			allowed := codeValue.Resources
			if len(allowed) == 0 {
				allowed = accessClient.Resources
			}

			if !resourcesAllowed([]string{accessTokenRequest.Resource}, allowed) {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_target",
					ErrorDescription: "requested resource not allowed",
					code:             http.StatusBadRequest,
				})

				return
			}

			resources = []string{accessTokenRequest.Resource}
		}

		user, err := m.GetOrCreateUser(r.Context(), data.GetUserRequest{
			Alias:         codeValue.Alias,
			AddScopeRoles: true,
		})
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		// nonce from the original authorization request lands in the id_token
		m.writeTokenWithOptions(w, r, user, clientID, codeValue.Scope, accessClient.Scope, tokenIssueOptions{
			Nonce:      codeValue.Nonce,
			Resources:  resources,
			RememberMe: accessTokenRequest.RememberMe,
		})

		return
	case grantTypeDeviceCode:
		m.deviceCodeGrant(w, r, accessTokenRequest, clientID, clientSecret)

		return
	case grantTypeTokenExchange:
		m.tokenExchangeGrant(w, r, accessTokenRequest, clientID, clientSecret)

		return
	case grantTypeEmailCode:
		m.emailCodeGrant(w, r, accessTokenRequest, clientID, clientSecret)

		return
	default:
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unsupported_grant_type",
			ErrorDescription: "grant type not supported",
			code:             http.StatusBadRequest,
		})

		return
	}
}

// APICerts returns the JWKS document.
func (m *Auth) APICerts(w http.ResponseWriter, r *http.Request) {
	signer, err := m.jwtRuntime(r.Context())
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	jwk := JWK{
		KID: signer.KID,
		KTY: "RSA",
		ALG: signer.JWT.Alg(),
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(signer.Public.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(signer.Public.E)).Bytes()),
	}

	w.Header().Set("Cache-Control", "public, max-age=300")

	httputil.JSON(w, http.StatusOK, JWKSResponse{Keys: []JWK{jwk}})
}

// APIUserInfo returns claims for a bearer access token.
func (m *Auth) APIUserInfo(w http.ResponseWriter, r *http.Request) {
	authorizationHeader := r.Header.Get("Authorization")
	tokenHeader := strings.TrimSpace(strings.TrimPrefix(authorizationHeader, "Bearer"))
	if tokenHeader == "" {
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
	if _, err := signer.JWT.Parse(tokenHeader, &claims,
		jwt.WithIssuer(m.issuerURL(r)), jwt.WithAudience("turna-auth")); err != nil {
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
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "user not found",
			code:             http.StatusUnauthorized,
		})

		return
	}

	claimsRet := map[string]any{
		"sub":                user.ID,
		"name":               user.Details["name"],
		"preferred_username": user.Details["name"],
	}

	if v, ok := user.Details["email"]; ok {
		claimsRet["email"] = v
	}
	if v, ok := user.Details["uid"]; ok {
		claimsRet["preferred_username"] = v
	}
	if v, ok := user.Details["family_name"]; ok {
		claimsRet["family_name"] = v
	}
	if v, ok := user.Details["given_name"]; ok {
		claimsRet["given_name"] = v
	}

	// apply custom userinfo templates selected by the {custom} path segment.
	if customName := r.PathValue("custom"); customName != "" {
		snap := m.cache.Snapshot()
		if !snap.CustomInfo.Disabled {
			if set, ok := snap.CustomInfo.Sets[customName]; ok {
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

// validateCustomInfoTemplates parses every custom_info template so syntax errors
// are rejected on save. It deliberately does NOT execute the templates: which
// claim/user fields exist depends on the deployment (LDAP attribute mapping,
// edited user details), so validating against fixed sample data would wrongly
// reject valid templates that reference deployment-specific fields. Use the
// preview endpoint to test rendering against real-shaped sample data.
func validateCustomInfoTemplates(cfg CustomInfoSettings) error {
	for name, set := range cfg.Sets {
		for k, tmpl := range set.Claims {
			if err := render.Validate(tmpl); err != nil {
				return fmt.Errorf("set %q claim %q: %w", name, k, err)
			}
		}
	}

	return nil
}

// customInfoPreviewRequest renders a custom_info set against sample data so the
// management UI can preview unsaved templates.
type customInfoPreviewRequest struct {
	Claims map[string]any `json:"claims"`
	User   *data.User     `json:"user"`
	Set    CustomInfoSet  `json:"set"`
}

// CustomInfoPreviewAPI applies an inline custom_info set to sample claims and
// returns the resulting claims, mirroring the templating in APIUserInfo
// (add, overwrite, or remove on empty render).
func (m *Auth) CustomInfoPreviewAPI(w http.ResponseWriter, r *http.Request) {
	var req customInfoPreviewRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode custom_info preview", err, http.StatusBadRequest))
		return
	}

	user := req.User
	if user == nil {
		user = &data.User{}
	}

	// base is the immutable template input; result is what we mutate/return.
	base := make(map[string]any, len(req.Claims))
	result := make(map[string]any, len(req.Claims))
	for k, v := range req.Claims {
		base[k] = v
		result[k] = v
	}

	tmplData := map[string]any{"claims": base, "user": user}
	for k, tmpl := range req.Set.Claims {
		out, err := render.ExecuteWithData(tmpl, tmplData)
		if err != nil {
			httputil.HandleError(w, httputil.NewError(fmt.Sprintf("claim %q: %s", k, err.Error()), err, http.StatusBadRequest))
			return
		}

		if len(out) == 0 {
			delete(result, k)

			continue
		}

		result[k] = string(out)
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{Payload: result})
}

// serverMetadata builds the shared OIDC discovery / RFC 8414 authorization
// server metadata document.
func (m *Auth) serverMetadata(r *http.Request, custom string) map[string]any {
	issuer := m.issuerURL(r)
	sn := m.cache.Snapshot()

	alg := "RS256"
	if signer, err := m.jwtRuntime(r.Context()); err == nil {
		alg = signer.JWT.Alg()
	}

	// issuer stays the shared /oauth2 value (matches the id_token iss); a
	// {custom} segment only points userinfo at the per-name custom_info route
	// so discovery-driven clients pick up the tailored claims automatically.
	userinfoEndpoint := issuer + "/userinfo"
	if custom != "" {
		userinfoEndpoint = issuer + "/userinfo/" + url.PathEscape(custom)
	}

	metadata := map[string]any{
		"issuer": issuer,
		"authorization_response_iss_parameter_supported": true,
		"authorization_endpoint":                         issuer + "/authorize",
		"token_endpoint":                                 issuer + "/token",
		"userinfo_endpoint":                              userinfoEndpoint,
		"jwks_uri":                                       issuer + "/certs",
		"device_authorization_endpoint":                  issuer + "/device_authorization",
		"revocation_endpoint":                            issuer + "/revoke",
		"introspection_endpoint":                         issuer + "/introspect",
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported": []string{
			"authorization_code", "client_credentials", "password", "refresh_token",
			grantTypeDeviceCode, grantTypeTokenExchange, grantTypeEmailCode,
		},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{alg},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
		"client_id_metadata_document_supported": true,
		"scopes_supported":                      []string{"openid"},
		"token_endpoint_auth_methods_supported": []string{
			"client_secret_basic", "client_secret_post", "none",
		},
		"revocation_endpoint_auth_methods_supported": []string{
			"client_secret_basic", "client_secret_post",
		},
		"introspection_endpoint_auth_methods_supported": []string{
			"client_secret_basic", "client_secret_post",
		},
	}

	if sn.Registration.Enabled {
		metadata["registration_endpoint"] = issuer + "/register"
	}

	return metadata
}

// APIWellKnown returns the OpenID configuration for this issuer.
func (m *Auth) APIWellKnown(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, m.serverMetadata(r, r.PathValue("custom")))
}

// APIAuthServerMetadata returns the RFC 8414 authorization server metadata.
// It serves both the path-suffix variant under the issuer and the RFC-style
// root path-insertion variant (/.well-known/oauth-authorization-server/...).
func (m *Auth) APIAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, m.serverMetadata(r, ""))
}

// IssuerURL exposes the canonical external issuer URL of this auth
// instance (session.InfIssuerURL); session middlewares use it to derive
// authorization_servers of their RFC 9728 protected resource metadata.
func (m *Auth) IssuerURL(r *http.Request) string {
	return m.issuerURL(r)
}

func (m *Auth) issuerURL(r *http.Request) string {
	// oauth2.base_url is the canonical external origin of this auth
	// instance. Use it for the issuer as well as upstream code callbacks so
	// tokens issued through one application host can be refreshed through
	// another host without an issuer mismatch.
	if m.cache != nil {
		if baseURL := strings.TrimSpace(m.cache.Snapshot().OAuth2.BaseURL); baseURL != "" {
			if parsed, err := url.Parse(baseURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
				return fmt.Sprintf("%s://%s%s/oauth2", parsed.Scheme, parsed.Host, m.PrefixPath)
			}
		}
	}

	scheme := r.Header.Get("X-Forwarded-Proto")
	host := r.Header.Get("X-Forwarded-Host")

	if host == "" {
		host = r.Host
	}
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	return fmt.Sprintf("%s://%s%s/oauth2", scheme, host, m.PrefixPath)
}

// redirectURIAllowed checks a redirect target against client whitelists.
// With a client_id the client's whitelist applies (empty list allows all).
// Without a client_id the URI must match some whitelist when at least one
// client defines one; fully whitelist-free setups stay open for
// backwards compatibility.
func (m *Auth) redirectURIAllowed(clientID, redirectURI string) bool {
	if redirectURI == "" {
		return false
	}

	sn := m.cache.Snapshot()

	if clientID != "" {
		if client, ok := sn.OAuthClients[clientID]; ok {
			return client.redirectURIAllowedForClient(redirectURI)
		}

		// service account fallback client
		if user, err := m.cache.GetUser(data.GetUserRequest{
			Alias:          clientID,
			ServiceAccount: &data.True,
		}); err == nil {
			whitelistURLs, _ := user.Details["whitelist_urls"].(string)

			return redirectAllowed(redirectURI, splitFields(whitelistURLs))
		}

		return false
	}

	anyWhitelist := false
	for _, client := range sn.OAuthClients {
		if len(client.WhitelistURLs) == 0 {
			continue
		}

		anyWhitelist = true
		if redirectAllowed(redirectURI, client.WhitelistURLs) {
			return true
		}
	}

	return !anyWhitelist
}

// pkceParams reads and validates RFC 7636 parameters from the query.
func pkceParams(r *http.Request) (string, string, error) {
	challenge := r.URL.Query().Get("code_challenge")
	method := r.URL.Query().Get("code_challenge_method")

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

// verifyPKCE checks the verifier against the stored challenge.
func verifyPKCE(challenge, method, verifier string) bool {
	switch method {
	case "S256":
		sum := sha256.Sum256([]byte(verifier))

		return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
	default: // plain
		return subtle.ConstantTimeCompare([]byte(verifier), []byte(challenge)) == 1
	}
}

// APIAuth starts the authorization code flow against an upstream provider.
func (m *Auth) APIAuth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("response_type") != "code" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unsupported_response_type",
			ErrorDescription: "response type not supported",
			code:             http.StatusBadRequest,
		})

		return
	}

	providerName := r.PathValue("provider")
	providerCfg, ok := m.cache.Snapshot().OAuthProviders[providerName]
	if !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: fmt.Sprintf("provider %q not found", providerName),
			code:             http.StatusNotFound,
		})

		return
	}

	if !m.redirectURIAllowed(r.URL.Query().Get("client_id"), r.URL.Query().Get("redirect_uri")) {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "redirect_uri not allowed",
			code:             http.StatusBadRequest,
		})

		return
	}

	codeChallenge, codeChallengeMethod, err := pkceParams(r)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	resources := r.URL.Query()["resource"]
	for _, resource := range resources {
		if err := validateResource(resource); err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_target",
				ErrorDescription: err.Error(),
				code:             http.StatusBadRequest,
			})

			return
		}
	}

	state := ulid.Make().String()

	stateValue, err := oauth2store.Encode(oauth2store.State{
		RedirectURI:         r.URL.Query().Get("redirect_uri"),
		State:               state,
		OrgState:            r.URL.Query().Get("state"),
		Scope:               strings.Fields(r.URL.Query().Get("scope")),
		Nonce:               r.URL.Query().Get("nonce"),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ClientID:            r.URL.Query().Get("client_id"),
		Resources:           resources,
	})
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	codeStore, err := m.codeStoreRuntime(r.Context())
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	if err := codeStore.State.Set(r.Context(), state, stateValue); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	code, err := m.codeRuntime()
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	authCodeURL, err := code.AuthCodeURL(r, state, providerName, providerCfg.Session())
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	httputil.Redirect(w, http.StatusTemporaryRedirect, authCodeURL)
}

// APICodeAuth handles the upstream provider callback and issues a local code.
func (m *Auth) APICodeAuth(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")
	providerCfg, ok := m.cache.Snapshot().OAuthProviders[providerName]
	if !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: fmt.Sprintf("provider %q not found", providerName),
			code:             http.StatusNotFound,
		})

		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "code or state not found",
			code:             http.StatusBadRequest,
		})

		return
	}

	codeStore, err := m.codeStoreRuntime(r.Context())
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	stateRaw, ok, err := codeStore.State.Get(r.Context(), state)
	if err != nil || !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state not found",
			code:             http.StatusBadRequest,
		})

		return
	}

	stateValue, err := oauth2store.Decode[oauth2store.State](stateRaw)
	if err != nil || stateValue.State != state {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state not match",
			code:             http.StatusBadRequest,
		})

		return
	}

	provider := providerCfg.Session()

	codeClient, err := m.codeRuntime()
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	tokenValue, statusCode, err := codeClient.CodeToken(r.Context(), r, code, providerName, provider)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             statusCode,
		})

		return
	}

	claims, err := m.claimsFromProviderToken(r, tokenValue, provider)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	// keep historic alias selection: userinfo responses use the email,
	// jwt claims prefer the username
	aliasKeys := []string{"preferred_username", "email", "name"}
	if provider.UserInfoURL != "" {
		aliasKeys = []string{"email"}
	}

	alias, err := aliasFromClaims(claims, aliasKeys...)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	// claim mapping: auto-register and role sync, same model as LDAP
	if err := m.syncFederatedUser(r.Context(), alias, claims, providerCfg.ClaimMapping); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	codeID := ulid.Make().String()

	codeValue, err := oauth2store.Encode(oauth2store.Code{
		Alias:               alias,
		Scope:               stateValue.Scope,
		Nonce:               stateValue.Nonce,
		CodeChallenge:       stateValue.CodeChallenge,
		CodeChallengeMethod: stateValue.CodeChallengeMethod,
		// bind the code to the requesting client when it identified itself;
		// RedirectURI stays empty here to keep older redeemers working.
		ClientID:  stateValue.ClientID,
		Resources: stateValue.Resources,
	})
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	if err := codeStore.Code.Set(r.Context(), "code_"+codeID, codeValue); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	urlParsed, err := url.Parse(stateValue.RedirectURI)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	urlParsed.RawQuery = url.Values{
		"code":  {codeID},
		"state": {stateValue.OrgState},
	}.Encode()

	httputil.Redirect(w, http.StatusTemporaryRedirect, urlParsed.String())
}
