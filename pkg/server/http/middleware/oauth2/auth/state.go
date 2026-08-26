package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const stateBindingCookiePrefix = "turna_oauth_state_"

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

// StateBindingHash returns the value stored alongside OAuth state. The raw
// browser binding only exists in the short-lived, host-only cookie.
func StateBindingHash(binding string) string {
	sum := sha256.Sum256([]byte(binding))

	return hex.EncodeToString(sum[:])
}

// SetStateBindingCookie binds one OAuth state value to the browser that
// started the flow. A state-derived name allows concurrent login flows.
func SetStateBindingCookie(w http.ResponseWriter, state, binding, path string, secure bool, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateBindingCookieName(state),
		Value:    binding,
		Path:     path,
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearStateBindingCookie expires the browser binding after the state has
// been consumed, whether the callback ultimately succeeds or fails.
func ClearStateBindingCookie(w http.ResponseWriter, state, path string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateBindingCookieName(state),
		Path:     path,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ValidStateBinding checks that the callback arrived in the browser that
// initiated the corresponding authorization request.
func ValidStateBinding(r *http.Request, state, expectedHash string) bool {
	if expectedHash == "" {
		return false
	}

	cookie, err := r.Cookie(stateBindingCookieName(state))
	if err != nil {
		return false
	}

	actualHash := StateBindingHash(cookie.Value)

	return subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) == 1
}

// RequestIsHTTPS recognizes direct TLS and the external scheme selected by
// the OAuth callback URL builder. Treating a spoofed header as HTTPS fails
// closed because the resulting Secure cookie will not return over HTTP.
func RequestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil || strings.EqualFold(r.URL.Scheme, "https") {
		return true
	}

	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])

	return strings.EqualFold(forwardedProto, "https")
}

func stateBindingCookieName(state string) string {
	sum := sha256.Sum256([]byte(state))

	return stateBindingCookiePrefix + hex.EncodeToString(sum[:])
}
