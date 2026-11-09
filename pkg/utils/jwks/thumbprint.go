package jwks

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// PublicJWK is a parsed public JWK with its RFC 7638 thumbprint.
type PublicJWK struct {
	Key any
	// Thumbprint is the base64url SHA-256 JWK thumbprint (RFC 7638).
	Thumbprint string
}

// ParsePublicJWK parses a single public JWK, as embedded in a DPoP proof
// header. Private key members are refused.
func ParsePublicJWK(raw json.RawMessage) (*PublicJWK, error) {
	var members map[string]any
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, fmt.Errorf("invalid jwk: %w", err)
	}

	for _, private := range []string{"d", "p", "q", "dp", "dq", "qi", "k"} {
		if _, ok := members[private]; ok {
			return nil, fmt.Errorf("jwk must not contain private key member %q", private)
		}
	}

	var k rawJWK
	if err := json.Unmarshal(raw, &k); err != nil {
		return nil, fmt.Errorf("invalid jwk: %w", err)
	}

	key, err := parseJWK(k)
	if err != nil {
		return nil, err
	}

	thumbprint, err := thumbprint(k)
	if err != nil {
		return nil, err
	}

	return &PublicJWK{Key: key, Thumbprint: thumbprint}, nil
}

// thumbprint computes the RFC 7638 thumbprint over the required members in
// lexicographic order.
func thumbprint(k rawJWK) (string, error) {
	var canonical string
	switch k.Kty {
	case "RSA":
		canonical = fmt.Sprintf(`{"e":%q,"kty":"RSA","n":%q}`, k.E, k.N)
	case "EC":
		canonical = fmt.Sprintf(`{"crv":%q,"kty":"EC","x":%q,"y":%q}`, k.Crv, k.X, k.Y)
	case "OKP":
		canonical = fmt.Sprintf(`{"crv":%q,"kty":"OKP","x":%q}`, k.Crv, k.X)
	default:
		return "", fmt.Errorf("kty %q: %w", k.Kty, ErrUnsupportedKeyType)
	}

	sum := sha256.Sum256([]byte(canonical))

	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
