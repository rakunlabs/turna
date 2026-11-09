package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	oauth2auth "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

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

	query := r.URL.Query()
	clientID := query.Get("client_id")
	if clientID == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "client_id is required",
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

	redirectURI := query.Get("redirect_uri")
	client, ok := m.federatedClient(clientID, redirectURI)
	if !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "redirect_uri not allowed",
			code:             http.StatusBadRequest,
		})

		return
	}

	codeChallenge, codeChallengeMethod, err := pkceParams(r, m.cache.Snapshot().Authorize.AllowPlainPKCE)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: err.Error(),
			code:             http.StatusBadRequest,
		})

		return
	}
	if client.ClientSecret == "" && codeChallenge == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "public clients require PKCE (code_challenge)",
			code:             http.StatusBadRequest,
		})

		return
	}

	resources := query["resource"]
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
	if !resourcesAllowed(resources, client.Resources) {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_target",
			ErrorDescription: "requested resource not allowed for this client",
			code:             http.StatusBadRequest,
		})

		return
	}

	state, err := randomHex(32)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}
	browserBinding, err := randomHex(32)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	stateValue, err := oauth2store.Encode(oauth2store.State{
		RedirectURI:         redirectURI,
		State:               state,
		OrgState:            query.Get("state"),
		Scope:               strings.Fields(query.Get("scope")),
		Nonce:               query.Get("nonce"),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ClientID:            clientID,
		Resources:           resources,
		BrowserBindingHash:  oauth2auth.StateBindingHash(browserBinding),
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
	if err := codeStore.State.Set(r.Context(), state, stateValue); err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}
	oauth2auth.SetStateBindingCookie(
		w,
		state,
		browserBinding,
		m.PrefixPath+"/oauth2/code/",
		oauth2auth.RequestIsHTTPS(r),
		oauth2store.DefaultStateTimeout,
	)

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

	stateRaw, ok, err := codeStore.TakeState(r.Context(), state)
	if err != nil || !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state not found",
			code:             http.StatusBadRequest,
		})

		return
	}
	oauth2auth.ClearStateBindingCookie(
		w,
		state,
		m.PrefixPath+"/oauth2/code/",
		oauth2auth.RequestIsHTTPS(r),
	)

	stateValue, err := oauth2store.Decode[oauth2store.State](stateRaw)
	if err != nil || stateValue.State != state {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state not match",
			code:             http.StatusBadRequest,
		})

		return
	}
	if !oauth2auth.ValidStateBinding(r, state, stateValue.BrowserBindingHash) {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state browser binding invalid",
			code:             http.StatusBadRequest,
		})

		return
	}
	stateClient, ok := m.federatedClient(stateValue.ClientID, stateValue.RedirectURI)
	if !ok || (stateClient.ClientSecret == "" && stateValue.CodeChallenge == "") {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "state client binding invalid",
			code:             http.StatusBadRequest,
		})

		return
	}
	if !resourcesAllowed(stateValue.Resources, stateClient.Resources) {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_target",
			ErrorDescription: "state resource not allowed for this client",
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

	// bind the login to a local user (provider+subject link, alias policy,
	// optional register) and sync mapped roles
	user, err := m.resolveFederatedUser(r.Context(), federatedLogin{
		Provider: "oauth2:" + providerName,
		Subject:  federatedSubject(claims, providerCfg.ClaimMapping, "sub"),
		Alias:    alias,
		Claims:   claims,
		Mapping:  providerCfg.ClaimMapping,
	})
	if err != nil {
		if errors.Is(err, errFederatedRefused) {
			slog.Warn("federated login refused", slog.String("provider", providerName), slog.String("alias", alias), slog.String("error", err.Error()))
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "access_denied",
				ErrorDescription: "this account cannot sign in here",
				code:             http.StatusForbidden,
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

	codeID, err := randomHex(32)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	codeValue, err := oauth2store.Encode(oauth2store.Code{
		Alias:               alias,
		UserID:              user.ID,
		Scope:               stateValue.Scope,
		Nonce:               stateValue.Nonce,
		CodeChallenge:       stateValue.CodeChallenge,
		CodeChallengeMethod: stateValue.CodeChallengeMethod,
		ClientID:            stateValue.ClientID,
		RedirectURI:         stateValue.RedirectURI,
		Resources:           stateValue.Resources,
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

	q := urlParsed.Query()
	q.Set("code", codeID)
	q.Set("iss", m.issuerURL(r))
	if stateValue.OrgState != "" {
		q.Set("state", stateValue.OrgState)
	}
	urlParsed.RawQuery = q.Encode()

	httputil.Redirect(w, http.StatusTemporaryRedirect, urlParsed.String())
}
