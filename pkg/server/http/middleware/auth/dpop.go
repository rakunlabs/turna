package auth

import (
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/dpop"
)

// DPoP (RFC 9449) support for the token and userinfo endpoints.

var dpopAlgorithms = dpop.Algorithms

type dpopContextKey struct{}

// dpopReplay is the fleet-wide replay check backed by the flow store.
func (m *Auth) dpopReplay(r *http.Request) dpop.ReplayFunc {
	if m.store == nil {
		return nil
	}

	return func(id string) (bool, error) {
		return m.store.CreateFlowCodeOnce(r.Context(), flowKindDPoPJTI, id, struct{}{}, dpop.MaxAge+dpop.Skew)
	}
}

// verifyDPoP validates the DPoP proof of a request to this auth instance.
func (m *Auth) verifyDPoP(r *http.Request, accessToken string) (*dpop.Proof, error) {
	return dpop.Verify(r, dpop.HTU(r, m.issuerURL(r)), accessToken, m.dpopReplay(r))
}

// requestDPoP returns the validated DPoP proof of a token request.
func requestDPoP(r *http.Request) *dpop.Proof {
	proof, _ := r.Context().Value(dpopContextKey{}).(*dpop.Proof)

	return proof
}

// checkTokenBinding enforces RFC 9449 §7 on a resource request to this
// instance (userinfo).
func (m *Auth) checkTokenBinding(r *http.Request, claims map[string]any, scheme, accessToken string) error {
	return dpop.CheckBinding(r, claims, scheme, accessToken, dpop.HTU(r, m.issuerURL(r)), m.dpopReplay(r))
}

// tokenJKT returns the cnf.jkt binding of token claims.
func tokenJKT(claims map[string]any) string {
	return dpop.JKT(claims)
}

// DPoPReplay exposes the shared replay cache to resource middlewares in the
// same process (oauth2_resource, session).
func (m *Auth) DPoPReplay(r *http.Request) dpop.ReplayFunc {
	return m.dpopReplay(r)
}
