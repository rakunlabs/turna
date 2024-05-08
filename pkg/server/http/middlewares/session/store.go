package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"github.com/rakunlabs/turna/pkg/server/http/middlewares/session/store"
)

type Store struct {
	Active string       `cfg:"active"`
	Redis  *store.Redis `cfg:"redis"`
	File   *store.File  `cfg:"file"`
}

var (
	TokenKey    = "token"
	ProviderKey = "provider"
)

type StoreInf interface {
	Get(r *http.Request, name string) (*sessions.Session, error)
}

func (m *Session) GetStore() StoreInf {
	return m.store
}

func (m *Session) SetStore(ctx context.Context) error {
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

func (m *Session) SetToken(c echo.Context, token []byte, providerName string) error {
	cookieValue := base64.StdEncoding.EncodeToString(token)

	// set the cookie
	session, _ := m.store.Get(c.Request(), m.GetCookieName(c))
	session.Values[TokenKey] = cookieValue
	session.Values[ProviderKey] = providerName

	if err := session.Save(c.Request(), c.Response()); err != nil {
		return err
	}

	// add header for session set
	c.Response().Header().Set("X-Session-Set", "true")

	return nil
}

func (m *Session) DelToken(c echo.Context) error {
	session, _ := m.store.Get(c.Request(), m.GetCookieName(c))
	session.Options.MaxAge = -1

	return session.Save(c.Request(), c.Response())
}
