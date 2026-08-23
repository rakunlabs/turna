package session

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"sync/atomic"

	"github.com/rakunlabs/ok"
	"golang.org/x/sync/singleflight"
)

const (
	CtxTokenHeaderKey     = "token_header"
	CtxTokenHeaderDelKey  = "token_header_delete"
	CtxDisableRedirectKey = "disable_redirect"
	CtxCookieNameKey      = "cookie_name"

	// CtxPublicAccessKey is set to true by the session middleware when the
	// request matched a permission flagged public on an auth_skip_paths auth.
	// A following iam_check reads it and passes the request without running
	// the same check again.
	CtxPublicAccessKey = "public_access"
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
	// ProviderSource pulls the provider list of an auth middleware
	// (UI-managed "session_providers" namespace) on top of the static
	// provider map; dynamic providers overlay same-named static ones.
	ProviderSource *ProviderSource `cfg:"provider_source"`
	// SetProvider is the default provider to set for refresing tokens.
	SetProvider string `cfg:"set_provider"`

	// SkipPaths lists request path patterns (doublestar globs, e.g.
	// "/auth/oauth2/**") that never require authentication: credentials are
	// still honored when present (claims context and X-User are set), but
	// anonymous requests pass through with identity headers stripped instead
	// of being redirected to login. Useful for public OAuth2/MCP endpoints
	// that live behind the same router as protected pages.
	SkipPaths []string `cfg:"skip_paths"`
	// ProtectedResource publishes this surface as an RFC 9728 OAuth2
	// protected resource: the metadata document is served under
	// /.well-known/oauth-protected-resource and 401 challenges point
	// discovery-driven clients (MCP) at it via WWW-Authenticate
	// resource_metadata. authorization_servers derive automatically from
	// providers backed by an in-process auth middleware.
	ProtectedResource *ProtectedResource `cfg:"protected_resource"`

	// AuthSkipPaths lists auth middlewares whose public surface is added to
	// skip_paths. An entry is either an in-process auth middleware name or
	// the check endpoint URL of a remote auth (e.g.
	// "https://idp.example.com/auth/check").
	//
	// A name contributes two things: the static public plane patterns
	// (/oauth2/**, /saml/**, discovery documents, ...) and a per-request
	// anonymous access check against permissions flagged public in the auth
	// UI. A URL contributes the anonymous public check only. Nothing is
	// added implicitly; a provider's auth_middleware setting has no effect
	// on skip paths.
	AuthSkipPaths []string `cfg:"auth_skip_paths"`

	store StoreInf `cfg:"-"`

	// authCheckClient posts anonymous public checks to URL entries of
	// AuthSkipPaths; nil when there are none.
	authCheckClient *ok.Client `cfg:"-"`

	authSkipOnce     sync.Once `cfg:"-"`
	authSkipPatterns []string  `cfg:"-"`

	// dynamic holds the provider_source state; nil until the first refresh.
	dynamic  atomic.Pointer[providerState] `cfg:"-"`
	dynamicM sync.Mutex                    `cfg:"-"`

	// refreshGroup collapses concurrent refreshes of the same token. Remote
	// providers may rotate refresh tokens as single-use credentials, so one
	// browser page load must not send the same token upstream several times.
	refreshGroup singleflight.Group `cfg:"-"`
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

	if err := m.InitProviderSource(); err != nil {
		return err
	}

	if err := m.SetAction(); err != nil {
		return err
	}

	if err := m.initAuthSkip(); err != nil {
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

// initAuthSkip prepares the HTTP client for remote check endpoint URLs
// listed in auth_skip_paths. No-op when every entry is an in-process name.
func (m *Session) initAuthSkip() error {
	hasURL := false
	for _, entry := range m.AuthSkipPaths {
		if isCheckURL(entry) {
			hasURL = true

			break
		}
	}

	if !hasURL {
		return nil
	}

	insecureSkipVerify := false
	if m.Action.Token != nil {
		insecureSkipVerify = m.Action.Token.InsecureSkipVerify
	}

	client, err := ok.New(
		ok.WithDisableRetry(true),
		ok.WithInsecureSkipVerify(insecureSkipVerify),
		ok.WithLogger(slog.Default()),
	)
	if err != nil {
		return fmt.Errorf("cannot create auth_skip_paths check client: %w", err)
	}

	m.authCheckClient = client

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
