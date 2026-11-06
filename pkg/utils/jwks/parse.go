package jwks

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
)

var ErrUnsupportedKeyType = errors.New("unsupported JWK key type")

type rawJWKS struct {
	Keys []rawJWK `json:"keys"`
}

type rawJWK struct {
	KID string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`

	// RSA
	N string `json:"n"`
	E string `json:"e"`

	// EC / OKP
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// parseJWK converts a raw JWK into a crypto key usable by golang-jwt.
func parseJWK(k rawJWK) (any, error) {
	switch k.Kty {
	case "RSA":
		return parseRSA(k)
	case "EC":
		return parseEC(k)
	case "OKP":
		return parseOKP(k)
	default:
		// symmetric "oct" keys are rejected: a published shared secret
		// would let anyone sign tokens
		return nil, fmt.Errorf("kty %q: %w", k.Kty, ErrUnsupportedKeyType)
	}
}

func parseRSA(k rawJWK) (*rsa.PublicKey, error) {
	if k.N == "" || k.E == "" {
		return nil, fmt.Errorf("RSA JWK %q is missing n or e", k.KID)
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("RSA JWK %q: invalid n: %w", k.KID, err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("RSA JWK %q: invalid e: %w", k.KID, err)
	}

	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() < 2 {
		return nil, fmt.Errorf("RSA JWK %q: invalid exponent", k.KID)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e.Int64()),
	}, nil
}

func parseEC(k rawJWK) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve

	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("EC JWK %q: crv %q: %w", k.KID, k.Crv, ErrUnsupportedKeyType)
	}

	if k.X == "" || k.Y == "" {
		return nil, fmt.Errorf("EC JWK %q is missing x or y", k.KID)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("EC JWK %q: invalid x: %w", k.KID, err)
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("EC JWK %q: invalid y: %w", k.KID, err)
	}

	size := (curve.Params().BitSize + 7) / 8
	if len(xBytes) > size || len(yBytes) > size {
		return nil, fmt.Errorf("EC JWK %q: coordinates are too long for %s", k.KID, k.Crv)
	}

	// uncompressed point 0x04 || X || Y, also checks it is on the curve
	point := make([]byte, 1+2*size)
	point[0] = 4
	copy(point[1+size-len(xBytes):1+size], xBytes)
	copy(point[1+2*size-len(yBytes):], yBytes)

	pub, err := ecdsa.ParseUncompressedPublicKey(curve, point)
	if err != nil {
		return nil, fmt.Errorf("EC JWK %q: point is not on curve %s: %w", k.KID, k.Crv, err)
	}

	return pub, nil
}

func parseOKP(k rawJWK) (ed25519.PublicKey, error) {
	if k.Crv != "Ed25519" {
		return nil, fmt.Errorf("OKP JWK %q: crv %q: %w", k.KID, k.Crv, ErrUnsupportedKeyType)
	}

	if k.X == "" {
		return nil, fmt.Errorf("OKP JWK %q is missing x", k.KID)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("OKP JWK %q: invalid x: %w", k.KID, err)
	}

	if len(xBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("OKP JWK %q: invalid Ed25519 public key size %d", k.KID, len(xBytes))
	}

	return ed25519.PublicKey(xBytes), nil
}
