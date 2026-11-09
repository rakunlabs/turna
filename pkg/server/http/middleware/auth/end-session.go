package auth

import (
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// APIEndSession implements OpenID Connect RP-Initiated Logout 1.0. The
// session family of the id_token_hint is revoked, so every access and
// refresh token issued with it stops working, and the browser is sent to a
// registered post_logout_redirect_uri.
//
// A login middleware in front still has to delete its own session cookie;
// its logout route can redirect here.
func (m *Auth) APIEndSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if err := r.ParseForm(); err != nil {
			m.renderLogout(w, http.StatusBadRequest, "Invalid logout request.")

			return
		}
	}
	params := r.Form
	if params == nil {
		params = r.URL.Query()
	}

	clientID := params.Get("client_id")
	redirectURI := params.Get("post_logout_redirect_uri")
	state := params.Get("state")

	var claims jwt.MapClaims
	if hint := params.Get("id_token_hint"); hint != "" {
		signer, err := m.jwtRuntime(r.Context())
		if err != nil {
			m.renderLogout(w, http.StatusInternalServerError, "Logout is temporarily unavailable.")

			return
		}

		claims = jwt.MapClaims{}
		// an expired id_token is a valid hint (RP-Initiated Logout §2)
		if _, err := signer.Parse(hint, &claims, jwt.WithoutClaimsValidation()); err != nil {
			m.renderLogout(w, http.StatusBadRequest, "The logout request is not valid.")

			return
		}
		if iss, _ := claims["iss"].(string); !m.AcceptsIssuer(iss) {
			m.renderLogout(w, http.StatusBadRequest, "The logout request is not valid.")

			return
		}

		hintClient := tokenClientID(claims)
		if clientID != "" && hintClient != "" && clientID != hintClient {
			m.renderLogout(w, http.StatusBadRequest, "The logout request is not valid.")

			return
		}
		if clientID == "" {
			clientID = hintClient
		}

		if sid, _ := claims["sid"].(string); sid != "" {
			sub, _ := claims["sub"].(string)
			payload := revokedToken{Subject: sub, ClientID: hintClient, Type: "Logout"}
			ttl := m.cache.Snapshot().Token.GetRefreshAbsoluteLifetime()
			if exp := claimInt64(claims["session_exp"]); exp > 0 {
				ttl = max(time.Until(time.Unix(exp, 0)), time.Minute)
			}
			if _, err := m.store.CreateFlowCodeOnce(r.Context(), flowKindRevokedSession, sid, payload, ttl); err != nil {
				m.renderLogout(w, http.StatusInternalServerError, "Logout is temporarily unavailable.")

				return
			}
			m.audit(r, AuditEvent{Event: auditLogout, Outcome: auditSuccess, Subject: sub, ClientID: hintClient, Detail: map[string]any{"sid": sid}})
		}
	}

	if redirectURI == "" {
		m.renderLogout(w, http.StatusOK, "")

		return
	}

	// the redirect target must be registered for the identified client
	client, ok := m.lookupClient(clientID)
	if clientID == "" || !ok || !slices.Contains(client.PostLogoutRedirectURIs, redirectURI) {
		m.renderLogout(w, http.StatusBadRequest, "The post logout redirect URI is not registered for this application.")

		return
	}

	target, err := url.Parse(redirectURI)
	if err != nil {
		m.renderLogout(w, http.StatusBadRequest, "The post logout redirect URI is not valid.")

		return
	}
	if state != "" {
		q := target.Query()
		q.Set("state", state)
		target.RawQuery = q.Encode()
	}

	httputil.Redirect(w, http.StatusFound, target.String())
}

func (m *Auth) renderLogout(w http.ResponseWriter, code int, errMessage string) {
	if errMessage == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>Signed out</title></head><body><main><h1>You are signed out</h1></main></body></html>`))

		return
	}

	m.renderConsent(w, code, consentPageData{Error: errMessage})
}
