package store

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/rakunlabs/ada/utils/sessions"
)

// ExpiresAtKey is the reserved session value holding the server-side expiry
// as unix seconds. Stores use it instead of the cookie max age, so a browser
// session cookie can still be backed by a long-lived server-side record.
const ExpiresAtKey = "_expires_at"

// ErrUnavailable wraps backend failures (network, I/O). A missing record or an
// invalid cookie is not an error: the store returns a new session instead, so
// callers can tell "not logged in" from "cannot check right now".
var ErrUnavailable = errors.New("session store unavailable")

func unavailable(err error) error {
	return errors.Join(ErrUnavailable, err)
}

func newSession(store sessions.Store, name string, opts sessions.Options) *sessions.Session {
	session := sessions.NewSession(store, name)
	session.Options = cloneOptions(opts)

	return session
}

func cloneOptions(opts sessions.Options) *sessions.Options {
	cloned := opts
	if cloned.Path == "" {
		cloned.Path = "/"
	}

	return &cloned
}

// expiresAt reads ExpiresAtKey from session values; JSON decoding yields
// float64, gob and fresh values int64.
func expiresAt(values map[string]any) (time.Time, bool) {
	var sec int64
	switch v := values[ExpiresAtKey].(type) {
	case int64:
		sec = v
	case int:
		sec = int64(v)
	case float64:
		sec = int64(v)
	case json.Number:
		sec, _ = v.Int64()
	}

	if sec <= 0 {
		return time.Time{}, false
	}

	return time.Unix(sec, 0), true
}

// serverTTL returns the server-side lifetime of the session: the explicit
// expiry when present, otherwise the cookie max age. Zero means no expiry.
func serverTTL(values map[string]any, maxAge int) time.Duration {
	if exp, ok := expiresAt(values); ok {
		return max(time.Until(exp), time.Second)
	}

	if maxAge > 0 {
		return time.Duration(maxAge) * time.Second
	}

	return 0
}

func setSessionCookie(w http.ResponseWriter, name, value string, opts *sessions.Options) {
	if opts == nil {
		opts = &sessions.Options{Path: "/"}
	}

	cookie := &http.Cookie{
		Name:        name,
		Value:       value,
		Path:        opts.Path,
		Domain:      opts.Domain,
		MaxAge:      opts.MaxAge,
		Secure:      opts.Secure,
		HttpOnly:    opts.HttpOnly,
		Partitioned: opts.Partitioned,
		SameSite:    opts.SameSite,
	}
	if cookie.MaxAge < 0 {
		cookie.Expires = time.Unix(1, 0)
	}

	http.SetCookie(w, cookie)
}
