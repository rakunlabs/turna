package auth

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
)

// authorizeFlow is the payload stored in auth_flow_codes for a pending
// browser-based authorization code login (consent screen).
type authorizeFlow struct {
	ClientID            string   `json:"client_id"`
	RedirectURI         string   `json:"redirect_uri"`
	Scope               []string `json:"scope"`
	State               string   `json:"state"`
	Nonce               string   `json:"nonce"`
	CodeChallenge       string   `json:"code_challenge,omitempty"`
	CodeChallengeMethod string   `json:"code_challenge_method,omitempty"`
	Resources           []string `json:"resources,omitempty"`
}

// lookupClient resolves client metadata without authenticating it; configured
// clients first, IAM service accounts as fallback.
func (m *Auth) lookupClient(clientID string) (*AccessClient, bool) {
	sn := m.cache.Snapshot()

	if client, ok := sn.OAuthClients[clientID]; ok {
		return &client, true
	}

	user, err := m.cache.GetUser(data.GetUserRequest{
		Alias:          clientID,
		ServiceAccount: &data.True,
	})
	if err != nil {
		return nil, false
	}

	secret, _ := user.Details["secret"].(string)
	scope, _ := user.Details["scope"].(string)
	whitelistURLs, _ := user.Details["whitelist_urls"].(string)

	return &AccessClient{
		ClientSecret:  secret,
		Scope:         splitFields(scope),
		WhitelistURLs: splitFields(whitelistURLs),
	}, true
}

// validateResource checks an RFC 8707 resource indicator: absolute URI
// without a fragment.
func validateResource(resource string) error {
	u, err := url.Parse(resource)
	if err != nil {
		return fmt.Errorf("resource %q is not a valid uri", resource)
	}

	if !u.IsAbs() || u.Fragment != "" {
		return fmt.Errorf("resource %q must be an absolute uri without fragment", resource)
	}

	return nil
}

// resourcesAllowed checks requested resources against the client's allowed
// list; an empty client list allows any valid resource.
func resourcesAllowed(requested, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}

	for _, res := range requested {
		ok := false
		for _, allow := range allowed {
			if allow != "" && strings.HasPrefix(res, allow) {
				ok = true

				break
			}
		}

		if !ok {
			return false
		}
	}

	return true
}

// authorizeErrorRedirect sends the browser back to the client with an OAuth
// error response (only used after the redirect_uri has been validated).
func authorizeErrorRedirect(w http.ResponseWriter, redirectURI, state, errCode, errDescription string) {
	target, err := url.Parse(redirectURI)
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "redirect_uri is not a valid uri",
			code:             http.StatusBadRequest,
		})

		return
	}

	q := target.Query()
	q.Set("error", errCode)
	if errDescription != "" {
		q.Set("error_description", errDescription)
	}
	if state != "" {
		q.Set("state", state)
	}
	target.RawQuery = q.Encode()

	httputil.Redirect(w, http.StatusFound, target.String())
}

// APIAuthorize implements the local authorization endpoint: it validates the
// request, stores a pending flow and sends the browser to the consent page.
// Identity comes from the session (X-User) on the consent page, so this
// endpoint itself is public.
func (m *Auth) APIAuthorize(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().Authorize
	if cfg.Disabled {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "authorization endpoint is disabled",
			code:             http.StatusNotFound,
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

	client, ok := m.lookupClient(clientID)
	if !ok {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: fmt.Sprintf("client %q not found", clientID),
			code:             http.StatusBadRequest,
		})

		return
	}

	redirectURI := query.Get("redirect_uri")
	if !client.redirectURIAllowedForClient(redirectURI) {
		// per RFC 6749 §4.1.2.1 an invalid redirect_uri must NOT be
		// redirected to; show the error directly.
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "invalid_request",
			ErrorDescription: "redirect_uri not allowed",
			code:             http.StatusBadRequest,
		})

		return
	}

	state := query.Get("state")

	if query.Get("response_type") != "code" {
		authorizeErrorRedirect(w, redirectURI, state, "unsupported_response_type", "only response_type=code is supported")

		return
	}

	codeChallenge, codeChallengeMethod, err := pkceParams(r)
	if err != nil {
		authorizeErrorRedirect(w, redirectURI, state, "invalid_request", err.Error())

		return
	}

	// public clients prove possession with PKCE; without a secret the code
	// would otherwise be bearer-redeemable by anyone.
	if client.ClientSecret == "" && codeChallenge == "" {
		authorizeErrorRedirect(w, redirectURI, state, "invalid_request", "public clients require PKCE (code_challenge)")

		return
	}

	resources := query["resource"]
	for _, resource := range resources {
		if err := validateResource(resource); err != nil {
			authorizeErrorRedirect(w, redirectURI, state, "invalid_target", err.Error())

			return
		}
	}

	if !resourcesAllowed(resources, client.Resources) {
		authorizeErrorRedirect(w, redirectURI, state, "invalid_target", "requested resource not allowed for this client")

		return
	}

	flowID := ulid.Make().String()
	flow := authorizeFlow{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               strings.Fields(query.Get("scope")),
		State:               state,
		Nonce:               query.Get("nonce"),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Resources:           resources,
	}

	if err := m.store.CreateFlowCode(r.Context(), flowKindAuthorize, flowID, flow, cfg.GetFlowLifetime()); err != nil {
		authorizeErrorRedirect(w, redirectURI, state, "server_error", err.Error())

		return
	}

	httputil.Redirect(w, http.StatusFound, m.PrefixPath+"/oauth2/consent?flow="+url.QueryEscape(flowID))
}

var consentTemplate = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorize {{.ClientName}}</title>
<style>
:root { color-scheme: light dark; }
body { font-family: system-ui, sans-serif; display: flex; justify-content: center; padding-top: 8vh; margin: 0; background: #f3f4f6; color: #111827; }
@media (prefers-color-scheme: dark) { body { background: #111827; color: #f9fafb; } .card { background: #1f2937 !important; } }
.card { background: #fff; border-radius: 8px; padding: 2rem; max-width: 26rem; width: 100%; box-shadow: 0 1px 6px rgba(0,0,0,.15); }
h1 { font-size: 1.2rem; margin: 0 0 1rem; }
.scopes { margin: 1rem 0; padding-left: 1.2rem; }
.actions { display: flex; gap: .75rem; margin-top: 1.5rem; }
button { flex: 1; padding: .6rem 1rem; border-radius: 6px; border: 1px solid transparent; font-size: 1rem; cursor: pointer; }
.approve { background: #2563eb; color: #fff; }
.deny { background: transparent; border-color: #9ca3af; color: inherit; }
.muted { color: #6b7280; font-size: .85rem; }
.error { color: #dc2626; }
</style>
</head>
<body>
<div class="card">
{{- if .Error}}
<h1 class="error">Authorization error</h1>
<p>{{.Error}}</p>
{{- else}}
<h1><strong>{{.ClientName}}</strong> wants to access your account</h1>
<p class="muted">Signed in as <strong>{{.User}}</strong></p>
{{- if .Scopes}}
<p>Requested access:</p>
<ul class="scopes">
{{- range .Scopes}}
<li>{{.}}</li>
{{- end}}
</ul>
{{- else}}
<p>No additional scopes requested.</p>
{{- end}}
{{- if .Resources}}
<p class="muted">For resource:
{{- range .Resources}} <code>{{.}}</code>{{- end}}</p>
{{- end}}
<form method="post" action="{{.Action}}">
<input type="hidden" name="flow" value="{{.Flow}}">
<div class="actions">
<button class="deny" type="submit" name="action" value="deny">Deny</button>
<button class="approve" type="submit" name="action" value="approve">Allow</button>
</div>
</form>
{{- end}}
</div>
</body>
</html>
`))

type consentPageData struct {
	ClientName string
	User       string
	Scopes     []string
	Resources  []string
	Flow       string
	Action     string
	Error      string
}

func (m *Auth) renderConsent(w http.ResponseWriter, code int, data consentPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)

	_ = consentTemplate.Execute(w, data)
}

// consentUser resolves the identity for the consent plane from the X-User
// header set by a session middleware in front (a skip_paths session still
// authenticates cookie-carrying browsers on skipped routes). Anonymous
// browsers are redirected to authorize.login_url when allowRedirect is set.
//
// It returns the user alias; when it returns "", the response has already
// been written.
func (m *Auth) consentUser(w http.ResponseWriter, r *http.Request, flowID string, allowRedirect bool) string {
	if userAlias := r.Header.Get("X-User"); userAlias != "" {
		return userAlias
	}

	// redirect_path is the parameter the login middleware's UI returns to
	// after a successful login (same convention as the session middleware's
	// RedirectToLogin).
	if loginURL := m.cache.Snapshot().Authorize.LoginURL; allowRedirect && loginURL != "" {
		next := m.PrefixPath + "/oauth2/consent?flow=" + url.QueryEscape(flowID)
		sep := "?"
		if strings.Contains(loginURL, "?") {
			sep = "&"
		}

		httputil.Redirect(w, http.StatusFound, loginURL+sep+"redirect_path="+url.QueryEscape(next))

		return ""
	}

	m.renderConsent(w, http.StatusUnauthorized, consentPageData{Error: "You must be logged in to continue. Log in and retry from the application."})

	return ""
}

// ConsentAPI renders the consent page for a pending authorize flow. Identity
// comes from the configured session_middleware, or from the X-User header of
// an upstream session middleware.
func (m *Auth) ConsentAPI(w http.ResponseWriter, r *http.Request) {
	flowID := r.URL.Query().Get("flow")

	flow := authorizeFlow{}
	if err := m.store.GetFlowCode(r.Context(), flowKindAuthorize, flowID, &flow); err != nil {
		m.renderConsent(w, http.StatusNotFound, consentPageData{Error: "Authorization request not found or expired. Start over from the application."})

		return
	}

	userAlias := m.consentUser(w, r, flowID, true)
	if userAlias == "" {
		return
	}

	client, _ := m.lookupClient(flow.ClientID)

	// trusted first-party clients skip the consent screen
	if client != nil && client.SkipConsent {
		m.finishAuthorize(w, r, flowID, &flow, userAlias, true)

		return
	}

	clientName := flow.ClientID
	if client != nil && client.ClientName != "" {
		clientName = client.ClientName
	}

	m.renderConsent(w, http.StatusOK, consentPageData{
		ClientName: clientName,
		User:       userAlias,
		Scopes:     flow.Scope,
		Resources:  flow.Resources,
		Flow:       flowID,
		Action:     m.PrefixPath + "/oauth2/consent",
	})
}

// ConsentDecisionAPI handles the approve/deny form submission of the consent
// page as the X-User.
func (m *Auth) ConsentDecisionAPI(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		m.renderConsent(w, http.StatusBadRequest, consentPageData{Error: "Invalid form submission."})

		return
	}

	flowID := r.PostFormValue("flow")

	flow := authorizeFlow{}
	if err := m.store.GetFlowCode(r.Context(), flowKindAuthorize, flowID, &flow); err != nil {
		m.renderConsent(w, http.StatusNotFound, consentPageData{Error: "Authorization request not found or expired. Start over from the application."})

		return
	}

	// no login redirect on the POST plane; the form is only reachable from a
	// logged-in consent page
	userAlias := m.consentUser(w, r, flowID, false)
	if userAlias == "" {
		return
	}

	m.finishAuthorize(w, r, flowID, &flow, userAlias, r.PostFormValue("action") == "approve")
}

// finishAuthorize consumes the flow and either issues an authorization code
// bound to the client/redirect/PKCE/resource or reports the denial.
func (m *Auth) finishAuthorize(w http.ResponseWriter, r *http.Request, flowID string, flow *authorizeFlow, userAlias string, approved bool) {
	_ = m.store.DeleteFlowCode(r.Context(), flowKindAuthorize, flowID)

	if !approved {
		authorizeErrorRedirect(w, flow.RedirectURI, flow.State, "access_denied", "the user denied the request")

		return
	}

	// the alias must resolve to a known user before a code is issued
	if _, err := m.cache.GetUser(data.GetUserRequest{Alias: userAlias}); err != nil {
		m.renderConsent(w, http.StatusUnauthorized, consentPageData{Error: "Your session user is not known to this identity provider."})

		return
	}

	codeStore, err := m.codeStoreRuntime(r.Context())
	if err != nil {
		authorizeErrorRedirect(w, flow.RedirectURI, flow.State, "server_error", err.Error())

		return
	}

	codeID := ulid.Make().String()

	codeValue, err := oauth2store.Encode(oauth2store.Code{
		Alias:               userAlias,
		Scope:               flow.Scope,
		Nonce:               flow.Nonce,
		CodeChallenge:       flow.CodeChallenge,
		CodeChallengeMethod: flow.CodeChallengeMethod,
		ClientID:            flow.ClientID,
		RedirectURI:         flow.RedirectURI,
		Resources:           flow.Resources,
	})
	if err != nil {
		authorizeErrorRedirect(w, flow.RedirectURI, flow.State, "server_error", err.Error())

		return
	}

	if err := codeStore.Code.Set(r.Context(), "code_"+codeID, codeValue); err != nil {
		authorizeErrorRedirect(w, flow.RedirectURI, flow.State, "server_error", err.Error())

		return
	}

	target, err := url.Parse(flow.RedirectURI)
	if err != nil {
		m.renderConsent(w, http.StatusBadRequest, consentPageData{Error: "Invalid redirect target."})

		return
	}

	q := target.Query()
	q.Set("code", codeID)
	if flow.State != "" {
		q.Set("state", flow.State)
	}
	target.RawQuery = q.Encode()

	httputil.Redirect(w, http.StatusFound, target.String())
}
