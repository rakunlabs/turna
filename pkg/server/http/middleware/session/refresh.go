package session

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/request"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/store"
)

var (
	// errSessionUnavailable marks failures that say nothing about the session
	// itself: the token endpoint or the session store could not be reached.
	// The session is kept and the request can be retried.
	errSessionUnavailable = errors.New("session temporarily unavailable")

	errProviderNotFound = errors.New("provider not found")
)

// IsUnavailable reports whether err is a transient session failure. Callers
// must not delete the session for such errors; it is shared by every app
// validating against the same store and is likely still valid.
func IsUnavailable(err error) bool {
	return errors.Is(err, errSessionUnavailable) || errors.Is(err, store.ErrUnavailable)
}

// refreshRejected reports whether the token endpoint definitively refused the
// refresh token (invalid_grant, revoked, expired session).
func refreshRejected(err error) bool {
	if errors.Is(err, errProviderNotFound) {
		return true
	}

	var statusErr *request.StatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
			return true
		}
	}

	return false
}

// refreshSession refreshes the access token and stores the result. A failed
// refresh is only final when the provider rejected the token; when another
// request, replica or app sharing the session refreshed it in the meantime
// the stored token is used instead.
func (m *Session) refreshSession(w http.ResponseWriter, r *http.Request, providerName string, token *TokenData) (*TokenData, error) {
	refreshData, err := m.refreshTokenData(r, providerName, token)
	if err != nil {
		if current, ok := m.rotatedToken(r, token); ok {
			return current, nil
		}

		if refreshRejected(err) {
			return nil, err
		}

		return nil, errors.Join(errSessionUnavailable, err)
	}

	refreshed, err := ParseToken(refreshData)
	if err != nil {
		return nil, err
	}

	if err := m.SetToken(w, r, refreshData, providerName); err != nil {
		// the new access token is valid, so serve this request; the stored
		// session keeps the previous token and refreshes again next time
		slog.Warn("cannot store refreshed session", "error", err.Error())
	}

	return refreshed, nil
}

// rotatedToken re-reads the session and returns its token when it differs
// from the one that failed to refresh and does not need a refresh itself.
func (m *Session) rotatedToken(r *http.Request, token *TokenData) (*TokenData, bool) {
	v, err := m.store.Get(r, m.GetCookieName(r))
	if err != nil || v.IsNew {
		return nil, false
	}

	token64, _ := v.Values[TokenKey].(string)
	current, err := ParseToken64(token64)
	if err != nil || current.AccessToken == "" || current.AccessToken == token.AccessToken {
		return nil, false
	}

	if need, err := IsRefreshNeed(current.AccessToken); err != nil || need {
		return nil, false
	}

	return current, true
}

// sessionUnavailable answers a transient failure without touching the
// session cookie, so the client can simply retry.
func (m *Session) sessionUnavailable(w http.ResponseWriter, err error) {
	slog.Warn("session temporarily unavailable", "error", err.Error())

	w.Header().Set("Retry-After", "1")
	httputil.JSON(w, http.StatusServiceUnavailable, MetaData{Error: http.StatusText(http.StatusServiceUnavailable)})
}
