package session

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sync"
)

const (
	CtxTokenHeaderKey     = "token_header"
	CtxTokenHeaderDelKey  = "token_header_delete"
	CtxDisableRedirectKey = "disable_redirect"
	CtxCookieNameKey      = "cookie_name"
)

type Session struct {
	// SessionKey is the default signing key for configured session stores.
	// A store-specific session_key takes precedence when set.
	SessionKey string `cfg:"session_key"`
	Store      Store  `cfg:"store"`
	// Options for main cookie.
	Options Options `cfg:"options"`

	// CookieName for default cookie name.
	// Overwrite this value with 'cookie_name' ctx value.
	CookieName string `cfg:"cookie_name"`
	// CookieNameHosts for cookie name by host with regexp.
	CookieNameHosts []HostCookieName `cfg:"cookie_name_hosts"`

	Action   Action              `cfg:"action"`
	Provider map[string]Provider `cfg:"provider"`
	// SetProvider is the default provider to set for refresing tokens.
	SetProvider string `cfg:"set_provider"`

	// SkipPaths lists request path patterns (doublestar globs, e.g.
	// "/auth/oauth2/**") that never require authentication: credentials are
	// still honored when present (claims context and X-User are set), but
	// anonymous requests pass through with identity headers stripped instead
	// of being redirected to login. Useful for public OAuth2/MCP endpoints
	// that live behind the same router as protected pages.
	SkipPaths []string `cfg:"skip_paths"`
	// DisableIssuerSkipPaths turns off the automatic skip_paths that
	// in-process issuers (providers with auth_middleware) publish for their
	// public plane (/oauth2/**, discovery documents, ...). With this set,
	// only the explicit skip_paths above apply.
	DisableIssuerSkipPaths bool `cfg:"disable_issuer_skip_paths"`

	store StoreInf `cfg:"-"`

	issuerSkipOnce  sync.Once `cfg:"-"`
	issuerSkipPaths []string  `cfg:"-"`
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

func (m *Session) Middleware(ctx context.Context, name string) (func(http.Handler) http.Handler, error) {
	if err := m.Init(ctx, name); err != nil {
		return nil, err
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.Do(next, w, r)
		})
	}, nil
}
