package session

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

const (
	CtxTokenHeaderKey     = "token_header"
	CtxDisableRedirectKey = "disable_redirect"
)

type Session struct {
	SessionKey string  `cfg:"session_key"`
	Store      Store   `cfg:"store"`
	Options    Options `cfg:"options"`

	CookieName string `cfg:"cookie_name"`
	ValueName  string `cfg:"value_name"`

	Actions     Actions     `cfg:"actions"`
	Information Information `cfg:"information"`

	store StoreInf `cfg:"-"`
}

type Options struct {
	Path     string `cfg:"path"`
	MaxAge   int    `cfg:"max_age"`
	Domain   string `cfg:"domain"`
	Secure   bool   `cfg:"secure"`
	HttpOnly bool   `cfg:"http_only"`
	// SameSite for Lax 2, Strict 3, None 4.
	SameSite http.SameSite `cfg:"same_site"`
}

func (m *Session) Init(ctx context.Context, name string) error {
	if err := m.SetStore(ctx); err != nil {
		return err
	}

	if m.ValueName == "" {
		m.ValueName = "data"
	}

	if m.CookieName == "" {
		m.CookieName = "auth_session"
	}

	GlobalRegistry.Set(name, m)

	if err := m.SetAction(); err != nil {
		return err
	}

	return nil
}

func (m *Session) Middleware(ctx context.Context, name string) (echo.MiddlewareFunc, error) {
	if err := m.Init(ctx, name); err != nil {
		return nil, err
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return m.Do(next, c)
		}
	}, nil
}
