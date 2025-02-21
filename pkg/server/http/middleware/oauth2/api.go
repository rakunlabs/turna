package oauth2

import (
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
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
	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/token"
)

func (m *Oauth2) MuxSet(prefix string) *chi.Mux {
	mux := chi.NewMux()

	mux.Get(prefix+"/auth/{provider}", m.APIAuth)
	mux.Get(prefix+"/code/{provider}", m.CodeAuth)
	mux.Post(prefix+"/token", m.APIToken)
	mux.Get(prefix+"/certs", m.APICerts)

	return mux
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

	stateValue, err := EncodeState(State{
		RedirectURI: r.URL.Query().Get("redirect_uri"),
		State:       state,
		OrgState:    r.URL.Query().Get("state"),
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

// CodeAuth will receive callback and generate new token and respond to redirection.
func (m *Oauth2) CodeAuth(w http.ResponseWriter, r *http.Request) {
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

	stateValue, err := DecodeState(stateRaw)
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

	v := jwt.MapClaims{}
	if _, _, err := token.ParseAccessToken(tokenValue, &v); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	alias, _ := v["email"].(string)
	if alias == "" {
		alias, _ = v["preferred_username"].(string)
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

	// save code to store
	if err := m.storeCache.Code.Set(r.Context(), "code_"+codeID, alias); err != nil {
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

		m.GenerateToken(w, userID, nil, clientID, strings.Split(scope, " "), accessClient.Scope)

		return
	case "client_credentials":
		user, err := m.iam.DB().GetUser(data.GetUserRequest{
			Alias:          accessTokenRequest.Username,
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
		if secret, _ := user.Details["secret"].(string); secret != accessTokenRequest.Password {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "password not match",
				code:             http.StatusUnauthorized,
			})

			return
		}

		scope, _ := user.Details["scope"].(string)

		m.GenerateToken(w, user.ID, user, clientID, nil, strings.Split(scope, " "))

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
		alias, ok, err := m.storeCache.Code.Get(r.Context(), "code_"+accessTokenRequest.Code)
		if err != nil || !ok {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "code not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		_ = m.storeCache.Code.Delete(r.Context(), "code_"+accessTokenRequest.Code)

		user, err := m.iam.DB().GetUser(data.GetUserRequest{
			Alias:         alias,
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

		m.GenerateToken(w, user.ID, user, clientID, nil, accessClient.Scope)

		return
	case "password":
		accessClient, errResponse := m.GetAccessClient(clientID, clientSecret)
		if errResponse != nil {
			httputil.HandleError(w, errResponse)

			return
		}

		user, err := m.iam.DB().GetUser(data.GetUserRequest{
			Alias: accessTokenRequest.Username,
		})
		if err != nil && !errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		if errors.Is(err, data.ErrNotFound) {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_grant",
				ErrorDescription: "user not found",
				code:             http.StatusUnauthorized,
			})

			return
		}

		if !user.Local {
			// check LDAP for external user
			ok, err := m.iam.LdapCheckPassword(accessTokenRequest.Username, accessTokenRequest.Password)
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

		m.GenerateToken(w, user.ID, user, clientID, nil, accessClient.Scope)

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
		return nil, &AccessTokenErrorResponse{
			Error:            "invalid_client",
			ErrorDescription: "client not found",
			code:             http.StatusBadRequest,
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
