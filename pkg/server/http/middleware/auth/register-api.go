package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// dynamicClientPrefix marks client ids created through RFC 7591.
const dynamicClientPrefix = "dcr-"

// token endpoint auth methods supported for registered clients.
var registrationAuthMethods = map[string]bool{
	"none":                true,
	"client_secret_basic": true,
	"client_secret_post":  true,
}

// grant types a dynamically registered client may use.
var registrationGrantTypes = map[string]bool{
	"authorization_code": true,
	"refresh_token":      true,
	grantTypeDeviceCode:  true,
}

// ClientRegistrationRequest is the RFC 7591 client metadata request.
type ClientRegistrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri"`
	Scope                   string   `json:"scope"`
}

// ClientRegistrationResponse is the RFC 7591 client information response.
type ClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	RegistrationAccessToken string   `json:"registration_access_token,omitempty"`
	RegistrationClientURI   string   `json:"registration_client_uri,omitempty"`
}

func sha256Hex(v string) string {
	sum := sha256.Sum256([]byte(v))

	return hex.EncodeToString(sum[:])
}

// validateRegistrationRedirectURI accepts absolute URIs without fragments;
// custom schemes (native apps) and loopback http are explicitly fine.
func validateRegistrationRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("redirect_uri %q is not a valid uri", raw)
	}

	if !u.IsAbs() {
		return fmt.Errorf("redirect_uri %q must be absolute", raw)
	}

	if u.Fragment != "" {
		return fmt.Errorf("redirect_uri %q must not contain a fragment", raw)
	}

	return nil
}

// dynamicClientExpired reports whether a dynamic registration is past the
// configured client lifetime.
func dynamicClientExpired(client AccessClient, lifetime time.Duration, now time.Time) bool {
	if !client.Dynamic || lifetime <= 0 || client.CreatedAt == 0 {
		return false
	}

	return time.Unix(client.CreatedAt, 0).Add(lifetime).Before(now)
}

// pruneDynamicClients removes expired dynamic registrations and returns the
// remaining dynamic client count.
func (m *Auth) pruneDynamicClients(r *http.Request) int {
	sn := m.cache.Snapshot()
	lifetime := sn.Registration.GetClientLifetime()
	now := time.Now()

	count := 0
	pruned := false
	for id, client := range sn.OAuthClients {
		if !client.Dynamic {
			continue
		}

		if dynamicClientExpired(client, lifetime, now) {
			if _, err := m.store.DeleteOAuthClient(r.Context(), id); err == nil {
				pruned = true

				continue
			}
		}

		count++
	}

	if pruned {
		_ = m.cache.Reload(r.Context())
	}

	return count
}

// APIRegister implements RFC 7591 dynamic client registration.
func (m *Auth) APIRegister(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().Registration
	if !cfg.Enabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "dynamic client registration is disabled",
			code:             http.StatusNotFound,
		})

		return
	}

	var req ClientRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client_metadata",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	if len(req.RedirectURIs) == 0 {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_redirect_uri",
			ErrorDescription: "redirect_uris is required",
			code:             http.StatusBadRequest,
		})

		return
	}

	for _, uri := range req.RedirectURIs {
		if err := validateRegistrationRedirectURI(uri); err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_redirect_uri",
				ErrorDescription: err.Error(),
				code:             http.StatusBadRequest,
			})

			return
		}
	}

	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "client_secret_basic"
	}

	if !registrationAuthMethods[authMethod] {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client_metadata",
			ErrorDescription: fmt.Sprintf("token_endpoint_auth_method %q not supported", authMethod),
			code:             http.StatusBadRequest,
		})

		return
	}

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}

	for _, grant := range grantTypes {
		if !registrationGrantTypes[grant] {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_client_metadata",
				ErrorDescription: fmt.Sprintf("grant_type %q not allowed for dynamic clients", grant),
				code:             http.StatusBadRequest,
			})

			return
		}
	}

	if count := m.pruneDynamicClients(r); count >= cfg.GetMaxClients() {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client_metadata",
			ErrorDescription: "too many registered clients",
			code:             http.StatusTooManyRequests,
		})

		return
	}

	clientID := dynamicClientPrefix + strings.ToLower(ulid.Make().String())

	clientSecret := ""
	if authMethod != "none" {
		secret, err := randomHex(32)
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "server_error",
				ErrorDescription: err.Error(),
				code:             http.StatusInternalServerError,
			})

			return
		}

		clientSecret = secret
	}

	registrationToken, err := randomHex(32)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	scope := splitFields(req.Scope)
	if len(scope) == 0 {
		scope = cfg.DefaultScope
	}

	now := time.Now()

	client := AccessClient{
		ClientSecret:      clientSecret,
		Scope:             scope,
		RedirectURIs:      req.RedirectURIs,
		ClientName:        req.ClientName,
		Public:            authMethod == "none",
		Dynamic:           true,
		RegistrationToken: sha256Hex(registrationToken),
		CreatedAt:         now.Unix(),
	}

	clientRaw, err := json.Marshal(client)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	if _, err := m.store.PutOAuthClient(r.Context(), clientID, clientRaw, true, "dcr"); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	// make the client usable immediately on this instance; other instances
	// converge through the version poll.
	if err := m.cache.Reload(r.Context()); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	secretExpires := int64(0)
	if lifetime := cfg.GetClientLifetime(); lifetime > 0 {
		secretExpires = now.Add(lifetime).Unix()
	}

	w.Header().Set("Cache-Control", "no-store")

	httputil.JSON(w, http.StatusCreated, ClientRegistrationResponse{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientIDIssuedAt:        now.Unix(),
		ClientSecretExpiresAt:   secretExpires,
		RedirectURIs:            req.RedirectURIs,
		TokenEndpointAuthMethod: authMethod,
		GrantTypes:              grantTypes,
		ResponseTypes:           []string{"code"},
		ClientName:              req.ClientName,
		Scope:                   strings.Join(scope, " "),
		RegistrationAccessToken: registrationToken,
		RegistrationClientURI:   m.issuerURL(r) + "/register/" + url.PathEscape(clientID),
	})
}

// registrationClient authorizes an RFC 7592 management request with the
// registration access token and returns the dynamic client.
func (m *Auth) registrationClient(w http.ResponseWriter, r *http.Request) (string, *AccessClient) {
	clientID := r.PathValue("client_id")

	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	if token == "" {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "registration access token required",
			code:             http.StatusUnauthorized,
		})

		return "", nil
	}

	client, ok := m.cache.Snapshot().OAuthClients[clientID]
	if !ok || !client.Dynamic || client.RegistrationToken == "" ||
		subtle.ConstantTimeCompare([]byte(client.RegistrationToken), []byte(sha256Hex(token))) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_token",
			ErrorDescription: "registration access token invalid",
			code:             http.StatusUnauthorized,
		})

		return "", nil
	}

	return clientID, &client
}

func (m *Auth) registrationResponse(clientID string, client *AccessClient, r *http.Request) ClientRegistrationResponse {
	authMethod := "client_secret_basic"
	if client.Public {
		authMethod = "none"
	}

	return ClientRegistrationResponse{
		ClientID:                clientID,
		ClientSecret:            client.ClientSecret,
		ClientIDIssuedAt:        client.CreatedAt,
		ClientSecretExpiresAt:   0,
		RedirectURIs:            client.RedirectURIs,
		TokenEndpointAuthMethod: authMethod,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              client.ClientName,
		Scope:                   strings.Join(client.Scope, " "),
		RegistrationClientURI:   m.issuerURL(r) + "/register/" + url.PathEscape(clientID),
	}
}

// APIRegisterGet implements RFC 7592 client read.
func (m *Auth) APIRegisterGet(w http.ResponseWriter, r *http.Request) {
	clientID, client := m.registrationClient(w, r)
	if client == nil {
		return
	}

	httputil.JSON(w, http.StatusOK, m.registrationResponse(clientID, client, r))
}

// APIRegisterUpdate implements RFC 7592 client update for a safe subset of
// metadata (redirect_uris, client_name, scope).
func (m *Auth) APIRegisterUpdate(w http.ResponseWriter, r *http.Request) {
	clientID, client := m.registrationClient(w, r)
	if client == nil {
		return
	}

	var req ClientRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_client_metadata",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}

	if len(req.RedirectURIs) > 0 {
		for _, uri := range req.RedirectURIs {
			if err := validateRegistrationRedirectURI(uri); err != nil {
				httputil.HandleError(w, AccessTokenErrorResponse{
					Error:            "invalid_redirect_uri",
					ErrorDescription: err.Error(),
					code:             http.StatusBadRequest,
				})

				return
			}
		}

		client.RedirectURIs = req.RedirectURIs
	}

	if req.ClientName != "" {
		client.ClientName = req.ClientName
	}

	if req.Scope != "" {
		client.Scope = splitFields(req.Scope)
	}

	clientRaw, err := json.Marshal(client)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	if _, err := m.store.PutOAuthClient(r.Context(), clientID, clientRaw, true, "dcr"); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	if err := m.cache.Reload(r.Context()); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	httputil.JSON(w, http.StatusOK, m.registrationResponse(clientID, client, r))
}

// APIRegisterDelete implements RFC 7592 client deprovisioning.
func (m *Auth) APIRegisterDelete(w http.ResponseWriter, r *http.Request) {
	clientID, client := m.registrationClient(w, r)
	if client == nil {
		return
	}

	if _, err := m.store.DeleteOAuthClient(r.Context(), clientID); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	_ = m.cache.Reload(r.Context())

	w.WriteHeader(http.StatusNoContent)
}
