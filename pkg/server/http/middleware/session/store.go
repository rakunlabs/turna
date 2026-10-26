package session

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/ada/utils/sessions"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/store"
)

type Store struct {
	Active string       `cfg:"active"`
	Redis  *store.Redis `cfg:"redis"`
	File   *store.File  `cfg:"file"`
}

var (
	TokenKey                = "token"
	ProviderKey             = "provider"
	AuthenticationMethodKey = "authentication_method"
)

const (
	AuthenticationMethodPassword = "password"
	AuthenticationMethodCode     = "code"
	AuthenticationMethodPasskey  = "passkey"
)

type StoreInf interface {
	Get(r *http.Request, name string) (*sessions.Session, error)
}

func (m *Session) GetStore() StoreInf {
	return m.store
}

func (m *Session) SetStore(ctx context.Context) error {
	if m.SessionKey != "" {
		if m.Store.Redis != nil && m.Store.Redis.SessionKey == "" {
			m.Store.Redis.SessionKey = m.SessionKey
		}
		if m.Store.File != nil && m.Store.File.SessionKey == "" {
			m.Store.File.SessionKey = m.SessionKey
		}
	}

	sessionOpts := sessions.Options{
		Path:   "/",
		MaxAge: 86400,
	}

	if m.Options.Path != "" {
		sessionOpts.Path = m.Options.Path
	}
	if m.Options.MaxAge != 0 {
		sessionOpts.MaxAge = m.Options.MaxAge
	}
	if m.Options.Domain != "" {
		sessionOpts.Domain = m.Options.Domain
	}
	if m.Options.Secure {
		sessionOpts.Secure = m.Options.Secure
	}
	if m.Options.HttpOnly {
		sessionOpts.HttpOnly = m.Options.HttpOnly
	}
	if m.Options.SameSite != 0 {
		sessionOpts.SameSite = m.Options.SameSite
	}

	var err error
	switch m.Store.Active {
	case "redis":
		if m.Store.Redis == nil {
			return fmt.Errorf("redis store is not configured")
		}

		m.store, err = m.Store.Redis.Store(ctx, sessionOpts)
		if err != nil {
			return err
		}

		return nil
	case "file":
		if m.Store.File == nil {
			return fmt.Errorf("file store is not configured")
		}

		m.store = m.Store.File.Store(sessionOpts)

		return nil
	case "":
		if m.Store.Redis != nil {
			m.store, err = m.Store.Redis.Store(ctx, sessionOpts)
			if err != nil {
				return err
			}

			return nil
		}

		if m.Store.File != nil {
			m.store = m.Store.File.Store(sessionOpts)

			return nil
		}

		return fmt.Errorf("no store configured")
	default:
		return fmt.Errorf("unknown store: %s", m.Store.Active)
	}
}

func (m *Session) SetToken(w http.ResponseWriter, r *http.Request, token []byte, providerName string) error {
	return m.setToken(w, r, token, providerName, "")
}

// SetLoginToken stores a token produced by an interactive login together with
// its trusted server-side method. Ordinary refreshes use SetToken and preserve
// this value in the existing session.
func (m *Session) SetLoginToken(w http.ResponseWriter, r *http.Request, token []byte, providerName, method string) error {
	return m.setToken(w, r, token, providerName, method)
}

func (m *Session) setToken(w http.ResponseWriter, r *http.Request, token []byte, providerName, method string) error {
	cookieValue := base64.StdEncoding.EncodeToString(token)

	if m.SetProvider != "" {
		providerName = m.SetProvider
	}

	// set the cookie
	session, err := m.store.Get(r, m.GetCookieName(r))
	if err != nil {
		// writing a fresh session here would orphan the stored one and log
		// the user out on every app sharing it
		return err
	}

	session.Values[TokenKey] = cookieValue
	session.Values[ProviderKey] = providerName
	if method != "" {
		session.Values[AuthenticationMethodKey] = method
	}

	if expiresAt, maxAge, ok := sessionLifetime(token, session.Options.MaxAge, time.Now()); ok {
		session.Values[store.ExpiresAtKey] = expiresAt.Unix()
		session.Options.MaxAge = maxAge
	} else {
		delete(session.Values, store.ExpiresAtKey)
	}

	if err := session.Save(r, w); err != nil {
		return err
	}

	// add header for session set
	w.Header().Set("X-Session-Set", "true")

	return nil
}

// sessionLifetime derives the session lifetime from the refresh token, so the
// stored session lives exactly as long as it can still be refreshed. Tokens
// from the auth middleware carry a remember_me claim; for them the cookie
// follows the refresh expiry too: a fixed 24h window from login for standard
// sessions, sliding up to the absolute lifetime for remembered ones. Tokens
// without the claim (external providers) keep the configured cookie max age.
//
// ok is false when the refresh expiry is unknown; the configured max age then
// applies to both the cookie and the stored record.
func sessionLifetime(raw []byte, defaultMaxAge int, now time.Time) (time.Time, int, bool) {
	token, err := ParseToken(raw)
	if err != nil || token.RefreshToken == "" {
		return time.Time{}, 0, false
	}

	var (
		expiresAt   time.Time
		hasRemember bool
	)

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(token.RefreshToken, claims); err == nil {
		if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
			expiresAt = exp.Time
		}
		_, hasRemember = claims["remember_me"].(bool)
	}

	if expiresAt.IsZero() && token.RefreshExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(token.RefreshExpiresIn) * time.Second)
	}

	if !expiresAt.After(now) {
		return time.Time{}, 0, false
	}

	if hasRemember {
		return expiresAt, int(math.Ceil(expiresAt.Sub(now).Seconds())), true
	}

	return expiresAt, defaultMaxAge, true
}

func (m *Session) GetAuthenticationMethod(r *http.Request) (string, error) {
	v, err := m.store.Get(r, m.GetCookieName(r))
	if err != nil {
		return "", err
	}
	if v.IsNew {
		return "", errNoSession
	}

	method, _ := v.Values[AuthenticationMethodKey].(string)

	return method, nil
}

func (m *Session) DelToken(w http.ResponseWriter, r *http.Request) error {
	session, err := m.store.Get(r, m.GetCookieName(r))
	if errors.Is(err, store.ErrUnavailable) {
		// the record cannot be read, so it cannot be deleted either; keep the
		// cookie instead of silently logging out every app sharing it
		return err
	}

	session.Options.MaxAge = -1

	return session.Save(r, w)
}
