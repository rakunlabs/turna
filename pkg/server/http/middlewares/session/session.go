package session

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/labstack/echo/v4"
)

const (
	CtxTokenHeaderKey     = "token_header"
	CtxTokenHeaderDelKey  = "token_header_delete"
	CtxDisableRedirectKey = "disable_redirect"
	CtxCookieNameKey      = "cookie_name"
)

type Session struct {
	Store Store `cfg:"store"`
	// Options for main cookie.
	Options Options `cfg:"options"`

	// CookieName for default cookie name.
	// Overwrite this value with 'cookie_name' ctx value.
	CookieName string `cfg:"cookie_name"`
	// CookieNameHosts for cookie name by host with regexp.
	CookieNameHosts []HostCookieName `cfg:"cookie_name_hosts"`

	Action   Action              `cfg:"action"`
	Provider map[string]Provider `cfg:"provider"`

	store StoreInf `cfg:"-"`
}

type HostCookieName struct {
	// Host as "localhost:8082"
	Host  string `cfg:"host"`
	Regex string `cfg:"regex"`

	CookieName string `cfg:"cookie_name"`

	rgx *regexp.Regexp
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

	if m.CookieName == "" {
		m.CookieName = "auth_session"
	}

	GlobalRegistry.Set(name, m)

	if err := m.SetAction(); err != nil {
		return err
	}

	for k, c := range m.CookieNameHosts {
		if c.Regex != "" {
			rgx, err := regexp.Compile(c.Regex)
			if err != nil {
				return fmt.Errorf("cookieNameHosts[%d].regex invalid: %w", k, err)
			}

			m.CookieNameHosts[k].rgx = rgx
		}
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
