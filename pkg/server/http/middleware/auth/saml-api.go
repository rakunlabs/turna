package auth

import (
	"encoding/xml"
	"net/http"
	"net/url"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
)

// SAMLMetadata serves the SP metadata document for a provider.
func (m *Auth) SAMLMetadata(w http.ResponseWriter, r *http.Request) {
	sp, _, err := m.samlServiceProvider(r, r.PathValue("provider"))
	if err != nil {
		httputil.HandleError(w, httputil.NewError("saml provider not available", err, http.StatusNotFound))
		return
	}

	buf, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot build metadata", err, http.StatusInternalServerError))
		return
	}

	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(buf)
}

// SAMLLogin starts a SAML login against the IdP; on success the user is
// redirected back to redirect_uri with a local authorization code.
func (m *Auth) SAMLLogin(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")

	sp, _, err := m.samlServiceProvider(r, providerName)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("saml provider not available", err, http.StatusNotFound))
		return
	}

	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		httputil.HandleError(w, httputil.NewError("redirect_uri is required", nil, http.StatusBadRequest))
		return
	}

	if !m.redirectURIAllowed(r.URL.Query().Get("client_id"), redirectURI) {
		httputil.HandleError(w, httputil.NewError("redirect_uri not allowed", nil, http.StatusBadRequest))
		return
	}

	codeChallenge, codeChallengeMethod, err := pkceParams(r)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("invalid pkce parameters", err, http.StatusBadRequest))
		return
	}

	authReq, err := sp.MakeAuthenticationRequest(
		sp.GetSSOBindingLocation("urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"),
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect",
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
	)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot build authn request", err, http.StatusInternalServerError))
		return
	}

	relayID := ulid.Make().String()

	relay := samlRelay{
		Provider:            providerName,
		RedirectURI:         redirectURI,
		OrgState:            r.URL.Query().Get("state"),
		Scope:               splitFields(r.URL.Query().Get("scope")),
		RequestID:           authReq.ID,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
	}

	if err := m.store.CreateFlowCode(r.Context(), flowKindSAMLRelay, relayID, relay, samlRelayTTL); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot save relay state", err, http.StatusInternalServerError))
		return
	}

	redirectURL, err := authReq.Redirect(relayID, sp)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot build redirect", err, http.StatusInternalServerError))
		return
	}

	httputil.Redirect(w, http.StatusTemporaryRedirect, redirectURL.String())
}

// SAMLACS handles the IdP assertion callback and issues a local code.
func (m *Auth) SAMLACS(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")

	sp, cfg, err := m.samlServiceProvider(r, providerName)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("saml provider not available", err, http.StatusNotFound))
		return
	}

	if err := r.ParseForm(); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot parse form", err, http.StatusBadRequest))
		return
	}

	relayID := r.PostFormValue("RelayState")
	if relayID == "" {
		httputil.HandleError(w, httputil.NewError("RelayState not found", nil, http.StatusBadRequest))
		return
	}

	relay := samlRelay{}
	if err := m.store.GetFlowCode(r.Context(), flowKindSAMLRelay, relayID, &relay); err != nil {
		httputil.HandleError(w, httputil.NewError("relay state not found or expired", nil, http.StatusBadRequest))
		return
	}

	_ = m.store.DeleteFlowCode(r.Context(), flowKindSAMLRelay, relayID)

	if relay.Provider != providerName {
		httputil.HandleError(w, httputil.NewError("relay state provider mismatch", nil, http.StatusBadRequest))
		return
	}

	assertion, err := sp.ParseResponse(r, []string{relay.RequestID})
	if err != nil {
		httputil.HandleError(w, httputil.NewError("saml response invalid", err, http.StatusBadRequest))
		return
	}

	alias := samlAlias(assertion, cfg.AliasAttribute)
	if alias == "" {
		httputil.HandleError(w, httputil.NewError("alias not found in assertion", nil, http.StatusBadRequest))
		return
	}

	// claim mapping: auto-register and role sync, same model as LDAP
	if err := m.syncFederatedUser(r.Context(), alias, samlClaims(assertion), cfg.ClaimMapping); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot sync federated user", err, http.StatusInternalServerError))
		return
	}

	codeStore, err := m.codeStoreRuntime(r.Context())
	if err != nil {
		httputil.HandleError(w, httputil.NewError("code store not available", err, http.StatusInternalServerError))
		return
	}

	codeID := ulid.Make().String()

	codeValue, err := oauth2store.Encode(oauth2store.Code{
		Alias:               alias,
		Scope:               relay.Scope,
		CodeChallenge:       relay.CodeChallenge,
		CodeChallengeMethod: relay.CodeChallengeMethod,
	})
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot encode code", err, http.StatusInternalServerError))
		return
	}

	if err := codeStore.Code.Set(r.Context(), "code_"+codeID, codeValue); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot save code", err, http.StatusInternalServerError))
		return
	}

	urlParsed, err := url.Parse(relay.RedirectURI)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("invalid redirect_uri", err, http.StatusBadRequest))
		return
	}

	urlParsed.RawQuery = url.Values{
		"code":  {codeID},
		"state": {relay.OrgState},
	}.Encode()

	httputil.Redirect(w, http.StatusTemporaryRedirect, urlParsed.String())
}

// ////////////////////////////////////////////////////////////////////
// saml provider config CRUD

func (m *Auth) ListSAMLProviders(w http.ResponseWriter, r *http.Request) {
	m.listConfigResources(w, r, m.store.ListSAMLProviders, "cannot list saml providers")
}

func (m *Auth) GetSAMLProvider(w http.ResponseWriter, r *http.Request) {
	m.getConfigResource(w, r, m.store.GetSAMLProvider, "cannot get saml provider")
}

func (m *Auth) PutSAMLProvider(w http.ResponseWriter, r *http.Request) {
	m.putConfigResource(w, r, m.store.PutSAMLProvider, "cannot save saml provider")
}

func (m *Auth) DeleteSAMLProvider(w http.ResponseWriter, r *http.Request) {
	m.deleteConfigResource(w, r, m.store.DeleteSAMLProvider, "cannot delete saml provider")
}
