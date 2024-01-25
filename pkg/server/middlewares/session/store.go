package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/middlewares/session/store"
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

func (s *Session) GetStore() StoreInf {
	return s.store
}

func (s *Session) SetStore(ctx context.Context) error {
	sessionOpts := sessions.Options{
		Path:   "/",
		MaxAge: 86400,
	}

	if s.Options.Path != "" {
		sessionOpts.Path = s.Options.Path
	}
	if s.Options.MaxAge != 0 {
		sessionOpts.MaxAge = s.Options.MaxAge
	}
	if s.Options.Domain != "" {
		sessionOpts.Domain = s.Options.Domain
	}
	if s.Options.Secure {
		sessionOpts.Secure = s.Options.Secure
	}
	if s.Options.HttpOnly {
		sessionOpts.HttpOnly = s.Options.HttpOnly
	}
	if s.Options.SameSite != 0 {
		sessionOpts.SameSite = s.Options.SameSite
	}

	var err error
	switch s.Store.Active {
	case "redis":
		if s.Store.Redis == nil {
			return fmt.Errorf("redis store is not configured")
		}

		s.store, err = s.Store.Redis.Store(ctx, sessionOpts)
		if err != nil {
			return err
		}

		return nil
	case "file":
		if s.Store.File == nil {
			return fmt.Errorf("file store is not configured")
		}

		s.store = s.Store.File.Store([]byte(s.SessionKey), sessionOpts)

		return nil
	case "":
		if s.Store.Redis != nil {
			s.store, err = s.Store.Redis.Store(ctx, sessionOpts)
			if err != nil {
				return err
			}

			return nil
		}

		if s.Store.File != nil {
			s.store = s.Store.File.Store([]byte(s.SessionKey), sessionOpts)

			return nil
		}

		return fmt.Errorf("no store configured")
	default:
		return fmt.Errorf("unknown store: %s", s.Store.Active)
	}
}

func (s *Session) SetToken(c echo.Context, token []byte, providerName string) error {
	cookieValue := base64.StdEncoding.EncodeToString(token)

	// set the cookie
	session, _ := s.store.Get(c.Request(), s.CookieName)
	session.Values[TokenKey] = cookieValue
	session.Values[ProviderKey] = providerName

	if err := session.Save(c.Request(), c.Response()); err != nil {
		return err
	}

	// add header for session set
	c.Response().Header().Set("X-Session-Set", "true")

	return nil
}

func (s *Session) DelToken(c echo.Context) error {
	session, _ := s.store.Get(c.Request(), s.CookieName)
	session.Options.MaxAge = -1

	return session.Save(c.Request(), c.Response())
}
