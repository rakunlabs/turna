package auth

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// NewState generates cryptographically secure random state with base64 URL encoding.
func NewState() (string, error) {
	cryptoRandBytes := make([]byte, 16)
	_, err := rand.Read(cryptoRandBytes)
	if err != nil {
		return "", err
	}

	base64State := strings.TrimRight(base64.URLEncoding.EncodeToString(cryptoRandBytes), "=")

	return base64State, nil
}
