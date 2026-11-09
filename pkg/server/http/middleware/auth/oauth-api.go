package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
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
	// TOTP (or a recovery code) for password and email-code grants.
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
	// RFC 6749 §5.2: token endpoint errors are 400 except invalid_client
	// (401). Grant failures used to answer 401, which clients and the OpenID
	// conformance suite treat as a client authentication failure.
	if e.code == http.StatusUnauthorized {
		switch e.Error {
		case "invalid_grant", "invalid_scope", "unauthorized_client", "unsupported_grant_type":
			return http.StatusBadRequest
		}
	}

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

// clientCredentials reads the client credentials of a token-style request:
// the Basic authorization header takes precedence over the request body.
func (m *Auth) clientCredentials(r *http.Request, req AccessTokenRequest) (string, string) {
	if clientID, clientSecret, ok := r.BasicAuth(); ok {
		return m.basicClientCredentials(clientID, clientSecret)
	}

	return req.ClientID, req.ClientSecret
}

// basicClientCredentials normalizes the values of a Basic authorization
// header. RFC 6749 §2.3.1 requires them to be form-urlencoded, which is what
// Go's own oauth2 client and this project's own client do, but plenty of
// clients send them verbatim. The decoded client id is therefore only used
// when the verbatim one names no client, and the secret is only decoded
// alongside it; clientSecretMatches applies the same fallback on its own for
// the far more common case of a plain id with an encoded secret.
func (m *Auth) basicClientCredentials(clientID, clientSecret string) (string, string) {
	decodedID, err := url.QueryUnescape(clientID)
	if err != nil || decodedID == clientID {
		return clientID, clientSecret
	}

	if m.clientIDKnown(clientID) || !m.clientIDKnown(decodedID) {
		return clientID, clientSecret
	}

	decodedSecret, err := url.QueryUnescape(clientSecret)
	if err != nil {
		return decodedID, clientSecret
	}

	return decodedID, decodedSecret
}

// clientIDKnown reports whether a client id resolves to a client without
// authenticating it.
func (m *Auth) clientIDKnown(clientID string) bool {
	if clientID == "" {
		return false
	}

	if _, ok := m.lookupClient(clientID); ok {
		return true
	}

	return isClientMetadataURL(clientID)
}

// rolesClaimPath resolves the dot path the scope-derived roles are written to
// for a client: the per-client override when set, otherwise the global token
// setting (which defaults to the flat "roles" claim). Both the token endpoint
// and the userinfo endpoint go through here so a client never has to look for
// its roles in two different places.
func rolesClaimPath(sn *Snapshot, clientID string) string {
	path := sn.Token.GetRolesClaim()
	if client, ok := sn.OAuthClients[clientID]; ok && client.RolesClaim != "" {
		path = client.RolesClaim
	}

	return path
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
	// NoRefresh issues no refresh token (token exchange results must not
	// outlive or be renewed independently of the subject token's session).
	NoRefresh bool
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
	// Every grant converges here. A disabled user or service account must not
	// receive a new access, refresh or ID token through any authentication path.
	if user == nil || user.Disabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "principal is disabled",
			code:             http.StatusUnauthorized,
		})

		return
	}

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
		options.SID, err = randomHex(32)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}
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
	accessJTI, err := randomHex(32)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	claimsAccess := map[string]any{
		"iss":                issuer,
		"aud":                tokenAudience(options.Resources),
		"jti":                accessJTI,
		"sub":                user.ID,
		"azp":                clientID,
		"sid":                options.SID,
		"auth_time":          options.AuthTime,
		"session_exp":        options.SessionExpires,
		"remember_me":        options.RememberMe,
		"name":               user.Details["name"],
		"preferred_username": preferredUsername(user),
	}

	if v, ok := user.Details["email"]; ok {
		claimsAccess["email"] = v
	}

	// RFC 9449 §6: a DPoP proof on the token request binds the tokens
	tokenType := "Bearer"
	if proof := requestDPoP(r); proof != nil {
		claimsAccess["cnf"] = map[string]any{"jkt": proof.JKT}
		tokenType = "DPoP"
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

	// rolesClaim is resolved once and reused for the id_token below so an
	// OIDC client reads the roles from the same dot path in both tokens.
	rolesClaim := rolesClaimPath(sn, clientID)

	if len(rolesList) > 0 {
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
	if !options.NoRefresh && (refreshToken == "" || options.RememberMe) {
		refreshJTI, err := randomHex(32)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		// remembered sessions slide by their own idle window; standard
		// sessions keep the fixed refresh_lifetime window from login
		idle := tokenCfg.GetRefreshLifetime()
		if options.RememberMe {
			idle = tokenCfg.GetRememberIdleLifetime()
		}

		refreshExpires = min(now.Add(idle).Unix(), options.SessionExpires)
		claimsRefresh := map[string]any{
			"iss":         issuer,
			"aud":         tokenAudience(options.Resources),
			"jti":         refreshJTI,
			"sub":         user.ID,
			"azp":         clientID,
			"typ":         "Refresh",
			"sid":         options.SID,
			"auth_time":   options.AuthTime,
			"session_exp": options.SessionExpires,
			"remember_me": options.RememberMe,
		}
		if cnf, ok := claimsAccess["cnf"]; ok {
			claimsRefresh["cnf"] = cnf
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
			"iss":       m.issuerURL(r),
			"aud":       clientID,
			"azp":       clientID,
			"sub":       user.ID,
			"sid":       options.SID,
			"auth_time": options.AuthTime,
			"iat":       time.Now().Unix(),
		}
		if !tokenCfg.OIDCStandardClaims {
			// historic marker; the typ claim is not defined for id_tokens
			claimsID["typ"] = "ID"

			for k, v := range userStandardClaims(user, scopeMap, false) {
				claimsID[k] = v
			}
		} else {
			// OIDC Core §5.4: when an access token is issued, scope claims
			// are returned from the userinfo endpoint, not in the id_token
			claimsID["typ"] = nil
		}

		if options.Nonce != "" {
			claimsID["nonce"] = options.Nonce
		}

		// Roles belong in the id_token too: OIDC clients (and libraries that
		// only ever look at the id_token, which is the common default) would
		// otherwise see an authenticated user with no authorization data and
		// silently fall back to "no roles". rolesList is already scope-derived,
		// so this leaks nothing the access token would not have carried.
		if len(rolesList) > 0 {
			setClaimByPath(claimsID, rolesClaim, rolesList)
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
		TokenType:             tokenType,
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

	clientID, clientSecret := m.clientCredentials(r, accessTokenRequest)
	if clientID == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: "client credentials not provided",
			code:             http.StatusBadRequest,
		})

		return
	}

	// RFC 9449: a DPoP proof binds the issued tokens to the client's key
	if r.Header.Get("DPoP") != "" {
		proof, err := m.verifyDPoP(r, "")
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_dpop_proof", ErrorDescription: err.Error(), code: http.StatusBadRequest})

			return
		}
		r = r.WithContext(context.WithValue(r.Context(), dpopContextKey{}, proof))
	} else if client, ok := m.lookupClient(clientID); ok && client.RequireDPoP {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_dpop_proof", ErrorDescription: "this client requires DPoP", code: http.StatusBadRequest})

		return
	}

	if client, ok := m.lookupClient(clientID); ok && !client.allowsGrant(accessTokenRequest.GrantType) {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "unauthorized_client", ErrorDescription: "grant type is not allowed for this client", code: http.StatusBadRequest})
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
					Error:            "invalid_client",
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
				Error:            "invalid_client",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		if secret, _ := user.Details["secret"].(string); !clientSecretMatches(secret, clientSecret) {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_client",
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

		user, err := m.cache.GetUser(userReq)
		if err != nil {
			if !errors.Is(err, data.ErrNotFound) {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "server_error",
					ErrorDescription: err.Error(),
					code:             http.StatusInternalServerError,
				})

				return
			}
			if passwordCfg.LdapRegisterDisabled || !m.ldapEnabled() {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: "user not found",
					code:             http.StatusBadRequest,
				})

				return
			}
			user = nil
		}

		if user != nil && user.ServiceAccount {
			// service accounts have no directory entry, an LDAP user with the
			// same name must not get their tokens
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "password login is not allowed for service accounts",
				code:             http.StatusBadRequest,
			})

			return
		}

		if user == nil || !user.Local {
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
					code:             http.StatusBadRequest,
				})

				return
			}

			if user == nil {
				if err := m.LdapSync(r.Context(), false, accessTokenRequest.Username); err != nil {
					httputil.HandleError(w, AccessTokenErrorResponse{
						Error:            "server_error",
						ErrorDescription: err.Error(),
						code:             http.StatusInternalServerError,
					})

					return
				}

				user, err = m.cache.GetUser(userReq)
				if err != nil {
					httputil.HandleError(w, AccessTokenErrorResponse{
						Error:            "invalid_grant",
						ErrorDescription: "user not found",
						code:             http.StatusBadRequest,
					})

					return
				}
			}
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
					code:             http.StatusBadRequest,
				})

				return
			}
		}

		if !m.requireLoginTOTP(w, r, user.ID, accessTokenRequest.TOTP) {
			return
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
		if err := m.parseOwnToken(signer, accessTokenRequest.RefreshToken, &claims); err != nil {
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

		// a DPoP-bound refresh token is only redeemable with the same key
		if jkt := tokenJKT(claims); jkt != "" {
			if proof := requestDPoP(r); proof == nil || proof.JKT != jkt {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_dpop_proof",
					ErrorDescription: "refresh token is bound to another DPoP key",
					code:             http.StatusBadRequest,
				})

				return
			}
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

		// remembered sessions rotate the refresh token; a rotated token
		// presented again after the grace window means it leaked
		if rememberMe {
			if grace := m.cache.Snapshot().Token.GetRefreshReuseGrace(); grace > 0 {
				reused, err := m.refreshReused(r.Context(), claims, jti, sid, exp.Time, grace)
				if err != nil {
					httputil.HandleError(w, AccessTokenErrorResponse{
						Error:            "server_error",
						ErrorDescription: err.Error(),
						code:             http.StatusInternalServerError,
					})

					return
				}
				if reused {
					httputil.HandleError(w, AccessTokenErrorResponse{
						Error:            "invalid_grant",
						ErrorDescription: "refresh token reuse detected",
						code:             http.StatusUnauthorized,
					})

					return
				}
			}
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
				sessionExpires = exp.Unix()
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
			RefreshExpires: exp.Unix(),
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

		codeRaw, ok, err := codeStore.TakeCode(r.Context(), "code_"+accessTokenRequest.Code)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: "code store: " + err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}
		if !ok {
			// codes are single use and expire after oauth2store.DefaultCodeTimeout;
			// a replayed code also revokes the tokens it was exchanged for
			m.revokeReusedCode(r.Context(), accessTokenRequest.Code)

			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "code not found, already used or expired",
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

		// Every authorization code is bound to the requesting client and
		// redirect target (RFC 6749 §4.1.3). Missing bindings are invalid too.
		if codeValue.ClientID == "" || codeValue.ClientID != clientID {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "code was issued to another client",
				code:             http.StatusUnauthorized,
			})

			return
		}

		if codeValue.RedirectURI == "" || codeValue.RedirectURI != accessTokenRequest.RedirectURI {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "redirect_uri not match",
				code:             http.StatusUnauthorized,
			})

			return
		}
		if accessClient.ClientSecret == "" && codeValue.CodeChallenge == "" {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "public clients require PKCE",
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

		userReq := data.GetUserRequest{Alias: codeValue.Alias, AddScopeRoles: true}
		if codeValue.UserID != "" {
			userReq = data.GetUserRequest{ID: codeValue.UserID, AddScopeRoles: true}
		}

		user, err := m.GetOrCreateUser(r.Context(), userReq)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		sid, err := randomHex(32)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}
		m.rememberCodeUse(r.Context(), accessTokenRequest.Code, codeUse{SID: sid, Subject: user.ID, ClientID: clientID})

		// nonce from the original authorization request lands in the id_token;
		// remember_me can come from the consent screen (stored in the code) or
		// from the token request itself
		m.writeTokenWithOptions(w, r, user, clientID, codeValue.Scope, accessClient.Scope, tokenIssueOptions{
			Nonce:      codeValue.Nonce,
			Resources:  resources,
			RememberMe: accessTokenRequest.RememberMe || codeValue.RememberMe,
			AuthTime:   codeValue.AuthTime,
			SID:        sid,
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
