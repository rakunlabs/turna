package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/worldline-go/turna/pkg/server/middlewares/session/store"
)

type Store struct {
	Active string       `cfg:"active"`
	Redis  *store.Redis `cfg:"redis"`
	File   *store.File  `cfg:"file"`
}

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

func SetSessionB64(r *http.Request, w http.ResponseWriter, body []byte, cookieName, valueName string, sessionStore StoreInf) (string, error) {
	cookieValue := base64.StdEncoding.EncodeToString(body)

	if err := SetSession(r, w, cookieValue, cookieName, valueName, sessionStore); err != nil {
		return "", err
	}

	return cookieValue, nil
}

func SetSession(r *http.Request, w http.ResponseWriter, value, cookieName, valueName string, sessionStore StoreInf) error {
	// set the cookie
	session, _ := sessionStore.Get(r, cookieName)
	session.Values[valueName] = value

	if err := session.Save(r, w); err != nil {
		return err
	}

	// add header for session set
	w.Header().Set("X-Session-Set", "true")

	return nil
}

func RemoveSession(r *http.Request, w http.ResponseWriter, cookieName string, sessionStore StoreInf) error {
	session, _ := sessionStore.Get(r, cookieName)
	session.Options.MaxAge = -1

	return session.Save(r, w)
}
