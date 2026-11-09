package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// federatedClient resolves the client that owns a federated login and checks
// its redirect target. Federated authorization codes must never be ownerless.
func (m *Auth) federatedClient(clientID, redirectURI string) (*AccessClient, bool) {
	if clientID == "" || redirectURI == "" {
		return nil, false
	}

	client, ok := m.lookupClient(clientID)
	if !ok || !client.redirectURIAllowedForClient(redirectURI) {
		return nil, false
	}

	return client, true
}

// pkceParams reads and validates RFC 7636 parameters from the query. Only
// S256 is accepted (OAuth 2.1, RFC 9700) unless allowPlain is set; a missing
// method means S256.
func pkceParams(r *http.Request, allowPlain bool) (string, string, error) {
	challenge := r.URL.Query().Get("code_challenge")
	method := r.URL.Query().Get("code_challenge_method")

	if challenge == "" {
		if method != "" {
			return "", "", fmt.Errorf("code_challenge_method without code_challenge")
		}

		return "", "", nil
	}

	switch method {
	case "", "S256":
		method = "S256"

		// RFC 7636 §4.2: BASE64URL(SHA256) is exactly 43 characters
		if len(challenge) != 43 || strings.ContainsAny(challenge, "+/=") {
			return "", "", fmt.Errorf("code_challenge must be a 43 character base64url S256 value")
		}
	case "plain":
		if !allowPlain {
			return "", "", fmt.Errorf("code_challenge_method plain is not allowed, use S256")
		}
	default:
		return "", "", fmt.Errorf("code_challenge_method %q not supported", method)
	}

	return challenge, method, nil
}

// verifyPKCE checks the verifier against the stored challenge. RFC 7636 §4.1
// limits the verifier to 43-128 unreserved characters.
func verifyPKCE(challenge, method, verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}

	switch method {
	case "S256":
		sum := sha256.Sum256([]byte(verifier))

		return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
	case "plain":
		return subtle.ConstantTimeCompare([]byte(verifier), []byte(challenge)) == 1
	}

	return false
}
