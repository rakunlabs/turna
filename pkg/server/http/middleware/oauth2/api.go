package oauth2

import (
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/ldap"
	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/token"
	"github.com/worldline-go/turna/pkg/server/model"
)

func (m *Oauth2) MuxSet(prefix string) *chi.Mux {
	mux := chi.NewMux()

	mux.Get(prefix+"/auth/{provider}", m.APIAuth)
	mux.Get(prefix+"/code/{provider}", m.APICodeAuth)
	mux.Post(prefix+"/token", m.APIToken)
	mux.Get(prefix+"/certs", m.APICerts)
	mux.Get(prefix+"/{custom}/.well-known/openid-configuration", m.APIWellKnown)
	mux.Get(prefix+"/userinfo", m.APIUserInfo)

	return mux
}

func (m *Oauth2) APIUserInfo(w http.ResponseWriter, r *http.Request) {
	authorizationHeader, ok := r.Header["Authorization"]
	if !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "access token not found",
			code:             http.StatusBadRequest,
		})

		return
	}

	tokenHeader := strings.TrimSpace(strings.TrimPrefix(authorizationHeader[0], "Bearer"))
	if tokenHeader == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "access token not found",
			code:             http.StatusBadRequest,
		})

		return
	}

	claims := jwt.MapClaims{}
	if _, err := m.jwt.Parse(tokenHeader, &claims); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: err.Error(),
			code:             http.StatusUnauthorized,
		})

		return
	}

	// create ID token response
	// find user
	sub, _ := claims["sub"].(string)
	if sub == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "user not found",
			code:             http.StatusUnauthorized,
		})

		return
	}

	user, err := m.iam.DB().GetUser(data.GetUserRequest{
		ID: sub,
	})
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "user not found",
			code:             http.StatusUnauthorized,
		})

		return
	}

	// //////////////////////////////////////////

	claimsRet := map[string]interface{}{
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

	// //////////////////////////////////////////
	httputil.JSON(w, http.StatusOK, claimsRet)
}

func (m *Oauth2) APIWellKnown(w http.ResponseWriter, r *http.Request) {
	customWell := chi.URLParam(r, "custom")
	if customWell != "" {
		if well, ok := m.WellKnown[customWell]; ok {
			httputil.JSON(w, http.StatusOK, well)

			return
		}
	}

	httputil.JSON(w, http.StatusFailedDependency, model.MetaData{Message: "not_found"})
}

func (m *Oauth2) APICerts(w http.ResponseWriter, r *http.Request) {
	jwk := JWK{
		KID: m.Token.KID,
		KTY: "RSA",
		ALG: m.jwt.Alg(),
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(m.Token.Cert.RSA.public.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(m.Token.Cert.RSA.public.E)).Bytes()),
	}

	if block, _ := pem.Decode([]byte(m.Token.Cert.RSA.PublicKey)); block != nil {
		jwk.X5C = []string{base64.StdEncoding.EncodeToString(block.Bytes)}
	}

	// Create the response
	response := JWKSResponse{
		Keys: []JWK{jwk},
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// Write the response
	httputil.JSON(w, http.StatusOK, response)
}

// APIAuth for auth endpoint implementation of oauth2.
//   - Authorization Code Grant
func (m *Oauth2) APIAuth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("response_type") != "code" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "unsupported_response_type",
			ErrorDescription: "response type not supported",
			code:             http.StatusBadRequest,
		})

		return
	}

	state, err := auth.NewState()
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	stateValue, err := store.Encode(store.State{
		RedirectURI: r.URL.Query().Get("redirect_uri"),
		State:       state,
		OrgState:    r.URL.Query().Get("state"),
		Scope:       strings.Fields(r.URL.Query().Get("scope")),
		Nonce:       r.URL.Query().Get("nonce"),
	})
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	m.storeCache.State.Set(r.Context(), state, stateValue)

	providerName := chi.URLParam(r, "provider")

	provider := m.Providers[providerName]
	if provider == nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: fmt.Sprintf("provider %q not found", providerName),
			code:             http.StatusNotFound,
		})

		return
	}

	authCodeURL, err := m.Code.AuthCodeURL(r, state, providerName, provider)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})
	}

	httputil.Redirect(w, http.StatusTemporaryRedirect, authCodeURL)
}

// APICodeAuth will receive callback and generate new token and respond to redirection.
func (m *Oauth2) APICodeAuth(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")

	provider := m.Providers[providerName]
	if provider == nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: fmt.Sprintf("provider %q not found", providerName),
			code:             http.StatusNotFound,
		})

		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "code not found",
			code:             http.StatusBadRequest,
		})

		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state not found",
			code:             http.StatusBadRequest,
		})

		return
	}

	stateRaw, ok, err := m.storeCache.State.Get(r.Context(), state)
	if err != nil || !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state not found",
			code:             http.StatusBadRequest,
		})

		return
	}

	stateValue, err := store.Decode[store.State](stateRaw)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	if stateValue.State != state {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state not match",
			code:             http.StatusBadRequest,
		})

		return
	}

	tokenValue, statusCode, err := m.Code.CodeToken(r.Context(), r, code, providerName, provider)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             statusCode,
		})

		return
	}

	accessToken, err := token.ParseAccessToken(tokenValue)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	defer func() {
		if provider.RevocationURL != "" {
			// revoke token
			if err := m.Code.RevokeToken(r.Context(), accessToken, provider); err != nil {
				slog.Error("failed to revoke token", "error", err)
			}
		}
	}()

	var alias string
	// if userinfo_endpoint is not empty, use it to get user info
	if provider.UserInfoURL != "" {
		userInfo, statusCode, err := m.Code.UserInfo(r.Context(), accessToken, provider)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: err.Error(),
				code:             statusCode,
			})

			return
		}

		for _, k := range []string{"email"} {
			if vAlias, _ := userInfo[k].(string); vAlias != "" {
				alias = vAlias
				break
			}
		}
	} else {
		v := jwt.MapClaims{}
		if _, _, err := token.ParseAccessTokenJWT(accessToken, &v); err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		for _, k := range []string{"preferred_username", "email", "name"} {
			if vAlias, _ := v[k].(string); vAlias != "" {
				alias = vAlias
				break
			}
		}
	}

	if alias == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: "alias not found",
			code:             http.StatusInternalServerError,
		})

		return
	}

	// create code flow response
	codeID := ulid.Make().String()

	codeValue, err := store.Encode(store.Code{
		Alias: alias,
		Scope: stateValue.Scope,
		Nonce: stateValue.Nonce,
	})
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	// save code to store
	if err := m.storeCache.Code.Set(r.Context(), "code_"+codeID, codeValue); err != nil {
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

	urlValue := urlParsed.String()

	httputil.Redirect(w, http.StatusTemporaryRedirect, urlValue)
}

// APIToken for token endpoint implementation of oauth2.
func (m *Oauth2) APIToken(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: "client credentials not provided",
			code:             http.StatusBadRequest,
		})

		return
	}

	// //////////////////////////////////////////////////////////////////////////
	// parse request
	accessTokenRequest := AccessTokenRequest{}
	if err := httputil.Decode(r, &accessTokenRequest); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	// //////////////////////////////////////////////////////////////////////////
	switch accessTokenRequest.GrantType {
	case "refresh_token":
		accessClient, errResponse := m.GetAccessClient(clientID, clientSecret)
		if errResponse != nil {
			httputil.HandleError(w, errResponse)

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

		claims := jwt.MapClaims{}
		if _, err := m.jwt.Parse(accessTokenRequest.RefreshToken, &claims); err != nil {
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
		if userID == "" {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		scope, _ := claims["scope"].(string)

		m.GenerateToken(w, userID, nil, clientID, strings.Split(scope, " "), accessClient.Scope, nil)

		return
	case "client_credentials":
		user, err := m.iam.DB().GetUser(data.GetUserRequest{
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
		}

		// check secret
		if secret, _ := user.Details["secret"].(string); secret != clientSecret {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "password not match",
				code:             http.StatusUnauthorized,
			})

			return
		}

		scope, _ := user.Details["scope"].(string)

		m.GenerateToken(w, user.ID, user, clientID, nil, strings.Split(scope, " "), nil)

		return
	case "authorization_code":
		accessClient, errResponse := m.GetAccessClient(clientID, clientSecret)
		if errResponse != nil {
			httputil.HandleError(w, errResponse)

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

		// get alias from code
		codeRaw, ok, err := m.storeCache.Code.Get(r.Context(), "code_"+accessTokenRequest.Code)
		if err != nil || !ok {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "code not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		codeValue, err := store.Decode[store.Code](codeRaw)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		_ = m.storeCache.Code.Delete(r.Context(), "code_"+accessTokenRequest.Code)

		user, err := m.iam.GetOrCreateUser(data.GetUserRequest{
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

		m.GenerateToken(w, user.ID, user, clientID, codeValue.Scope, accessClient.Scope, nil)

		return
	case "password":
		accessClient, errResponse := m.GetAccessClient(clientID, clientSecret)
		if errResponse != nil {
			httputil.HandleError(w, errResponse)

			return
		}

		userName := accessTokenRequest.Username
		if m.PassLower {
			userName = strings.ToLower(userName)
		}

		user, err := m.iam.GetOrCreateUser(data.GetUserRequest{
			Alias: userName,
		})
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: err.Error(),
				code:             http.StatusUnauthorized,
			})

			return
		}

		if !user.Local {
			// check LDAP for external user
			ok, err := m.iam.LdapCheckPassword(userName, accessTokenRequest.Password)
			if err != nil {
				if errors.Is(err, ldap.ErrExceedPasswordRetryLimit) {
					httputil.HandleError(w, AccessTokenErrorResponse{
						Error:            "invalid_grant",
						ErrorDescription: "exceed password retry limit",
						code:             http.StatusUnauthorized,
					})

					return
				}

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
		} else {
			// check local user
			password, ok := user.Details["password"].(string)
			if !ok {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "server_error",
					ErrorDescription: "password not found",
					code:             http.StatusInternalServerError,
				})

				return
			}

			if CompareBcrypt(password, accessTokenRequest.Password) != nil {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_grant",
					ErrorDescription: "password not match",
					code:             http.StatusUnauthorized,
				})

				return
			}
		}

		m.GenerateToken(w, user.ID, user, clientID, nil, accessClient.Scope, nil)

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

func (m *Oauth2) APILogout(w http.ResponseWriter, _ *http.Request) {
	httputil.NoContent(w, http.StatusNoContent)
}

func (m *Oauth2) GetAccessClient(clientID, clientSecret string) (*AccessClient, *AccessTokenErrorResponse) {
	accessClient, ok := m.AccessClients[clientID]
	if !ok {
		// try to fetch from IAM
		getAccess, err := m.iam.DB().GetUser(data.GetUserRequest{
			Alias:          clientID,
			ServiceAccount: &data.True,
		})
		if err != nil {
			return nil, &AccessTokenErrorResponse{
				Error:            "invalid_client",
				ErrorDescription: "client not found",
				code:             http.StatusBadRequest,
			}
		}

		secret, _ := getAccess.Details["secret"].(string)
		scope, _ := getAccess.Details["scope"].(string)
		whitelistURLs, _ := getAccess.Details["whitelist_urls"].(string)

		accessClient = AccessClient{
			ClientSecret:  secret,
			Scope:         strings.Split(scope, " "),
			WhitelistURLs: strings.Split(whitelistURLs, " "),
		}
	}

	if accessClient.ClientSecret != clientSecret {
		return nil, &AccessTokenErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "client secret not match",
			code:             http.StatusUnauthorized,
		}
	}

	return &accessClient, nil
}
