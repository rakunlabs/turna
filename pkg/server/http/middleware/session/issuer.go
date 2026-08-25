package session

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"

	"github.com/golang-jwt/jwt/v5"
)

// InfIssuer is an in-process token issuer, like the auth middleware.
//
// A provider configured with `auth_middleware: <name>` resolves the issuer
// from IssuerRegistry instead of calling cert_url/token_url over HTTP.
type InfIssuer interface {
	// Keyfunc returns the public key for tokens signed by this issuer.
	// It must return ErrKIDNotFound when the token was signed by someone else.
	Keyfunc(token *jwt.Token) (any, error)
	// IssueToken runs an OAuth2 token request (password, refresh_token, ...)
	// in-process and returns the raw JSON body with its HTTP status code.
	IssueToken(r *http.Request, form url.Values) ([]byte, int, error)
}

// InfRevoker is implemented by issuers that keep a token revocation list
// (RFC 7009). The login middleware revokes session tokens on logout when the
// provider's issuer supports it.
type InfRevoker interface {
	RevokeToken(ctx context.Context, token string) error
}

// InfAPIKey is implemented by issuers that can validate static API keys
// directly. It returns claim-shaped identity JSON for the key principal;
// no token exchange is involved and validation hits the issuer's database
// on every call, so deleted/disabled keys fail immediately.
type InfAPIKey interface {
	APIKeyData(ctx context.Context, key string) ([]byte, error)
}

// InfPublicPaths is implemented by issuers that publish the path patterns
// (doublestar globs) of their public plane — endpoints that must stay
// reachable for machine clients and pre-login browsers (token, callbacks,
// discovery documents, consent, ...).
//
// A session middleware lists issuers by name in `auth_skip_paths` to treat
// these patterns as skip_paths, so operators do not have to enumerate them
// by hand (and miss one).
type InfPublicPaths interface {
	PublicPathPatterns() []string
}

// InfCredentialPassthroughPaths publishes public endpoints that must receive
// raw credentials. Session bypasses optional authentication on these paths so
// it does not consume credentials before the owning middleware handles them.
type InfCredentialPassthroughPaths interface {
	CredentialPassthroughPathPatterns() []string
}

// InfSessionProviders is implemented by issuers whose session provider list
// is managed at runtime (the auth middleware's "session_providers" settings
// namespace). The returned version advances whenever the issuer's
// configuration changes, so callers can cache the map and rebuild derived
// state (keyfuncs) only on change.
type InfSessionProviders interface {
	SessionProviders() (map[string]Provider, uint64)
}

// InfSessionProviderGroups is implemented by issuers whose runtime-managed
// session provider list supports named groups (the auth middleware's
// "session_providers" namespace with `groups`). A session middleware
// configured with `provider_source.group` pulls only that group's providers.
type InfSessionProviderGroups interface {
	// SessionProvidersGroup returns the providers of one named group and the
	// issuer's configuration version; found is false when the group does not
	// exist.
	SessionProvidersGroup(group string) (providers map[string]Provider, version uint64, found bool)
}

// InfAccessChecker is implemented by in-process identity providers that can
// answer a host/path/method authorization check. An empty alias means an
// anonymous request and therefore checks public resources only.
type InfAccessChecker interface {
	AccessAllowed(ctx context.Context, alias, host, path, method string) (bool, error)
}

// InfPasskey is implemented by issuers that support WebAuthn (passkey)
// login. The body is the begin/finish JSON payload; the original request
// carries host/scheme information for relying-party derivation.
type InfPasskey interface {
	PasskeyToken(ctx context.Context, orig *http.Request, body []byte) ([]byte, int, error)
}

// Signup actions an issuer can run in-process for the login page.
const (
	SignupActionSignup       = "signup"
	SignupActionVerify       = "signup-verify"
	SignupActionReset        = "password-reset"
	SignupActionResetConfirm = "password-reset-confirm"
)

// SignupFeatures reports which self-service account flows are enabled on the
// issuer right now; the login page uses it to show/hide signup and
// forgot-password live without restarts.
type SignupFeatures struct {
	Signup            bool `json:"signup"`
	PasswordReset     bool `json:"password_reset"`
	PasswordMinLength int  `json:"password_min_length"`
}

// InfSignup is implemented by issuers that support self-registration and
// password reset over email (the auth middleware "signup" namespace).
type InfSignup interface {
	SignupFeatures() SignupFeatures
	// SignupAction runs one of the SignupAction* requests in-process; body is
	// the JSON payload including client credentials.
	SignupAction(ctx context.Context, action string, body []byte) ([]byte, int, error)
}

type issuerRegistry struct {
	store map[string]InfIssuer

	m sync.RWMutex
}

// IssuerRegistry holds in-process token issuers by middleware name.
var IssuerRegistry = &issuerRegistry{
	store: make(map[string]InfIssuer),
}

func (r *issuerRegistry) Set(name string, issuer InfIssuer) {
	r.m.Lock()
	defer r.m.Unlock()

	r.store[name] = issuer
}

func (r *issuerRegistry) Get(name string) InfIssuer {
	r.m.RLock()
	defer r.m.RUnlock()

	return r.store[name]
}

// issuerKeyFunc resolves issuer-backed providers lazily so that middleware
// initialization order does not matter; issuers register themselves when
// their middleware is built and requests only arrive after the server starts.
type issuerKeyFunc struct {
	// providers maps provider name -> issuer (middleware) name.
	providers map[string]string
}

func (i *issuerKeyFunc) Keyfunc(token *jwt.Token) (any, error) {
	for providerName, issuerName := range i.providers {
		issuer := IssuerRegistry.Get(issuerName)
		if issuer == nil {
			continue
		}

		key, err := issuer.Keyfunc(token)
		if err != nil {
			if errors.Is(err, ErrKIDNotFound) {
				continue
			}

			return nil, err
		}

		token.Header["provider_name"] = providerName

		return key, nil
	}

	return nil, ErrKIDNotFound
}
