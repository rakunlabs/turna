package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
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

// writeToken issues access+refresh tokens for the user.
func (m *Auth) writeToken(w http.ResponseWriter, r *http.Request, user *data.UserExtended, clientID string, scope, defScope []string) {
	m.writeTokenExt(w, r, user, clientID, scope, defScope, "", "")
}

// writeTokenExt issues access+refresh tokens for the user, optionally
// tagging the response with an RFC 8693 issued_token_type and embedding
// the OIDC nonce in the id_token.
func (m *Auth) writeTokenExt(w http.ResponseWriter, r *http.Request, user *data.UserExtended, clientID string, scope, defScope []string, issuedTokenType, nonce string) {
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

	tokenCfg := m.cache.Snapshot().Token

	claimsAccess := map[string]any{
		"aud":                "turna-auth",
		"sub":                user.ID,
		"azp":                clientID,
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
		claimsAccess["realm_access"] = map[string]any{
			"roles": rolesList,
		}
	}

	accessToken, err := signer.JWT.Generate(claimsAccess, nowAdd(tokenCfg.GetTokenLifetime()))
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	claimsRefresh := map[string]any{
		"aud": "turna-auth",
		"sub": user.ID,
		"azp": clientID,
		"typ": "Refresh",
	}

	if len(scopeList) > 0 {
		claimsRefresh["scope"] = strings.Join(scopeList, " ")
	}

	refreshToken, err := signer.JWT.Generate(claimsRefresh, nowAdd(tokenCfg.GetRefreshLifetime()))
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	// id_token for OIDC clients
	idToken := ""
	if _, ok := scopeMap["openid"]; ok {
		claimsID := map[string]any{
			"iss":                m.issuerURL(r),
			"aud":                clientID,
			"azp":                clientID,
			"sub":                user.ID,
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
		if nonce != "" {
			claimsID["nonce"] = nonce
		}

		idToken, err = signer.JWT.Generate(claimsID, nowAdd(tokenCfg.GetTokenLifetime()))
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
		ExpiresIn:             int64(tokenCfg.GetTokenLifetime().Seconds()),
		RefreshToken:          refreshToken,
		RefreshTokenExpiresIn: int64(tokenCfg.GetRefreshLifetime().Seconds()),
		Scope:                 strings.Join(scopeList, " "),
		IDToken:               idToken,
		IssuedTokenType:       issuedTokenType,
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

			m.writeToken(w, r, user, clientID, nil, splitFields(scope))

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

		m.writeToken(w, r, user, clientID, nil, splitFields(scope))

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

		m.writeToken(w, r, user, clientID, splitFields(accessTokenRequest.Scope), accessClient.Scope)

		return
	case "refresh_token":
		if _, err := m.GetAccessClient(clientID, clientSecret); err != nil {
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
		if _, err := signer.JWT.Parse(accessTokenRequest.RefreshToken, &claims); err != nil {
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

		scope, _ := claims["scope"].(string)

		m.writeToken(w, r, user, clientID, splitFields(scope), nil)

		return
	case "authorization_code":
		// PKCE clients may be public (no secret); the verifier check below
		// is the proof of possession
		var accessClient *AccessClient
		var err error
		if accessTokenRequest.CodeVerifier != "" {
			accessClient, err = m.resolveClient(clientID, clientSecret)
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
		m.writeTokenExt(w, r, user, clientID, codeValue.Scope, accessClient.Scope, "", codeValue.Nonce)

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
	if _, err := signer.JWT.Parse(tokenHeader, &claims); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: err.Error(),
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

	httputil.JSON(w, http.StatusOK, claimsRet)
}

// APIWellKnown returns the OpenID configuration for this issuer.
func (m *Auth) APIWellKnown(w http.ResponseWriter, r *http.Request) {
	issuer := m.issuerURL(r)

	alg := "RS256"
	if signer, err := m.jwtRuntime(r.Context()); err == nil {
		alg = signer.JWT.Alg()
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"issuer":                        issuer,
		"authorization_endpoint":        issuer + "/auth",
		"token_endpoint":                issuer + "/token",
		"userinfo_endpoint":             issuer + "/userinfo",
		"jwks_uri":                      issuer + "/certs",
		"device_authorization_endpoint": issuer + "/device_authorization",
		"response_types_supported":      []string{"code"},
		"grant_types_supported": []string{
			"authorization_code", "client_credentials", "password", "refresh_token",
			grantTypeDeviceCode, grantTypeTokenExchange, grantTypeEmailCode,
		},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{alg},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
	})
}

func (m *Auth) issuerURL(r *http.Request) string {
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
			return redirectAllowed(redirectURI, client.WhitelistURLs)
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

	state := ulid.Make().String()

	stateValue, err := oauth2store.Encode(oauth2store.State{
		RedirectURI:         r.URL.Query().Get("redirect_uri"),
		State:               state,
		OrgState:            r.URL.Query().Get("state"),
		Scope:               strings.Fields(r.URL.Query().Get("scope")),
		Nonce:               r.URL.Query().Get("nonce"),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
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
