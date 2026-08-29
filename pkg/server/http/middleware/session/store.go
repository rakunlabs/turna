package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

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
	session, _ := m.store.Get(r, m.GetCookieName(r))
	session.Values[TokenKey] = cookieValue
	session.Values[ProviderKey] = providerName
	if method != "" {
		session.Values[AuthenticationMethodKey] = method
	}

	if err := session.Save(r, w); err != nil {
		return err
	}

	// add header for session set
	w.Header().Set("X-Session-Set", "true")

	return nil
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
	session, _ := m.store.Get(r, m.GetCookieName(r))
	session.Options.MaxAge = -1

	return session.Save(r, w)
}
