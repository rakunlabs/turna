package oauth2

// JWKSResponse represents the JSON Web Key Set response structure.
type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key.
type JWK struct {
	KID string   `json:"kid"`
	KTY string   `json:"kty"`
	ALG string   `json:"alg"`
	Use string   `json:"use"`
	N   string   `json:"n"`
	E   string   `json:"e"`
	X5C []string `json:"x5c,omitempty"`
}
