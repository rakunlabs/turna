// Package jwt validates bearer JWTs with keys from a JWKS URL or static keys.
package jwt

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	oauth2claims "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
	"github.com/rakunlabs/turna/pkg/utils/jwks"
)

type JWT struct {
	// JWKSURL is the JWK Set of the issuer, e.g.
	// https://idp/.well-known/jwks.json.
	JWKSURL string `cfg:"jwks_url"`
	// RefreshInterval refetches the JWK Set, default 1h. Unknown key ids
	// refetch it at most once per minute.
	RefreshInterval time.Duration `cfg:"refresh_interval"`
	// PublicKeys are PEM public keys or files of them, used with or without
	// JWKSURL.
	PublicKeys []string `cfg:"public_keys"`
	// Secret verifies HS256/384/512 tokens. Avoid it for tokens of other
	// parties.
	Secret string `cfg:"secret" log:"-"`

	// Algorithms that are accepted, default the asymmetric ones of the keys
	// and HS* with Secret.
	Algorithms []string `cfg:"algorithms"`
	// Issuer must match the iss claim when set.
	Issuer []string `cfg:"issuer"`
	// Audience must contain one of these when set.
	Audience []string `cfg:"audience"`
	// Leeway for exp, nbf and iat, default 30s.
	Leeway time.Duration `cfg:"leeway"`
	// RequiredClaims must be present, values are checked when not empty;
	// a list claim matches when it contains the value.
	RequiredClaims map[string]string `cfg:"required_claims"`

	// TokenSource is where the token is read: header:<name>, query:<name>
	// or cookie:<name>, default header:Authorization with Bearer.
	TokenSource []string `cfg:"token_source"`
	// Optional lets requests without a token through; invalid tokens are
	// still rejected.
	Optional bool `cfg:"optional"`

	// ClaimHeaders sets request headers from claims, e.g.
	// X-User-Id: sub. Values sent by the client are removed.
	ClaimHeaders map[string]string `cfg:"claim_headers"`

	// RolesClaim are claims holding roles for role, in addition to roles,
	// realm_access.roles and resource_access.*.roles. A name is looked up
	// as is first, then as a dot separated path, e.g. groups,
	// https://example.com/roles or app.permissions.
	RolesClaim []string `cfg:"roles_claim"`
	// StripToken removes the token from the request before the next handler.
	StripToken bool `cfg:"strip_token"`
}

var (
	errNoToken    = errors.New("token missing")
	asymmetricAlg = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA"}
	hmacAlg       = []string{"HS256", "HS384", "HS512"}
)

type source struct {
	kind, name string
}

func (s source) get(r *http.Request) string {
	switch s.kind {
	case "header":
		v := r.Header.Get(s.name)
		if s.name == "Authorization" {
			scheme, token, ok := strings.Cut(v, " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") {
				return ""
			}

			return strings.TrimSpace(token)
		}

		return v
	case "query":
		return r.URL.Query().Get(s.name)
	case "cookie":
		if c, err := r.Cookie(s.name); err == nil {
			return c.Value
		}
	}

	return ""
}

func (s source) strip(r *http.Request) {
	switch s.kind {
	case "header":
		r.Header.Del(s.name)
	case "query":
		q := r.URL.Query()
		q.Del(s.name)
		r.URL.RawQuery = q.Encode()
	case "cookie":
		cookies := r.Cookies()
		r.Header.Del("Cookie")

		for _, c := range cookies {
			if c.Name != s.name {
				r.AddCookie(c)
			}
		}
	}
}

// remoteKeys loads a JWK Set lazily so an unreachable issuer does not stop
// Turna from starting.
type remoteKeys struct {
	ctx      context.Context
	url      string
	interval time.Duration

	mu        sync.Mutex
	set       *jwks.JWKS
	fetched   time.Time
	attempted time.Time
	fetching  bool
}

const minRefetch = time.Minute

// load returns the current JWK Set and fetches it again when it is stale or
// force is set, at most once per minRefetch. Only one request fetches; the
// others keep using the current set meanwhile.
func (k *remoteKeys) load(force bool) *jwks.JWKS {
	k.mu.Lock()

	now := time.Now()
	stale := k.set == nil || force || now.Sub(k.fetched) >= k.interval

	if !stale || k.fetching || now.Sub(k.attempted) < minRefetch {
		set := k.set
		k.mu.Unlock()

		return set
	}

	k.attempted = now
	k.fetching = true
	k.mu.Unlock()

	ctx, cancel := context.WithTimeout(k.ctx, 10*time.Second)
	set, err := jwks.Get(k.url, jwks.Options{Ctx: ctx})
	cancel()

	k.mu.Lock()
	defer k.mu.Unlock()

	k.fetching = false

	if err != nil {
		slog.Warn("jwt jwks fetch failed", "url", k.url, "error", err)
	} else {
		k.set = set
		k.fetched = now
	}

	return k.set
}

func (k *remoteKeys) keyfunc(t *gojwt.Token) (any, error) {
	set := k.load(false)
	if set == nil {
		return nil, errors.New("jwks unavailable")
	}

	key, err := set.Keyfunc(t)
	if errors.Is(err, jwks.ErrKIDNotFound) {
		// the issuer may have rotated its keys
		if set = k.load(true); set != nil {
			return set.Keyfunc(t)
		}
	}

	return key, err
}

func loadPublicKey(v string) (crypto.PublicKey, error) {
	data := []byte(v)
	if !strings.Contains(v, "-----BEGIN") {
		b, err := os.ReadFile(v)
		if err != nil {
			return nil, err
		}

		data = b
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block")
	}

	if block.Type == "CERTIFICATE" {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}

		return cert.PublicKey, nil
	}

	return x509.ParsePKIXPublicKey(block.Bytes)
}

func (m *JWT) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	if m.JWKSURL == "" && len(m.PublicKeys) == 0 && m.Secret == "" {
		return nil, errors.New("jwt needs jwks_url, public_keys or secret")
	}

	if m.RefreshInterval < 0 || m.Leeway < 0 {
		return nil, errors.New("jwt durations must not be negative")
	}

	var remote *remoteKeys
	if m.JWKSURL != "" {
		interval := m.RefreshInterval
		if interval == 0 {
			interval = time.Hour
		}

		remote = &remoteKeys{ctx: ctx, url: m.JWKSURL, interval: max(interval, minRefetch)}
	}

	static := make([]crypto.PublicKey, 0, len(m.PublicKeys))
	for i, v := range m.PublicKeys {
		key, err := loadPublicKey(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("jwt public key %d: %w", i, err)
		}

		static = append(static, key)
	}

	algorithms := m.Algorithms
	if len(algorithms) == 0 {
		if remote != nil || len(static) > 0 {
			algorithms = append(algorithms, asymmetricAlg...)
		}
		if m.Secret != "" {
			algorithms = append(algorithms, hmacAlg...)
		}
	}

	for _, alg := range algorithms {
		if alg == "none" {
			return nil, errors.New("jwt algorithm none is not allowed")
		}

		if slices.Contains(hmacAlg, alg) && m.Secret == "" {
			return nil, fmt.Errorf("jwt algorithm %s needs secret", alg)
		}
	}

	keyfunc := func(t *gojwt.Token) (any, error) {
		if slices.Contains(hmacAlg, t.Method.Alg()) {
			return []byte(m.Secret), nil
		}

		if _, hasKid := t.Header["kid"]; remote != nil && (hasKid || len(static) == 0) {
			return remote.keyfunc(t)
		}

		if len(static) == 0 {
			return nil, errors.New("no key")
		}

		keys := make([]gojwt.VerificationKey, len(static))
		for i, k := range static {
			keys[i] = k
		}

		return gojwt.VerificationKeySet{Keys: keys}, nil
	}

	leeway := m.Leeway
	if leeway == 0 {
		leeway = 30 * time.Second
	}

	opts := []gojwt.ParserOption{
		gojwt.WithValidMethods(algorithms),
		gojwt.WithLeeway(leeway),
		gojwt.WithExpirationRequired(),
		gojwt.WithIssuedAt(),
	}

	parser := gojwt.NewParser(opts...)

	sources := []source{{"header", "Authorization"}}
	if len(m.TokenSource) > 0 {
		sources = nil

		for _, v := range m.TokenSource {
			kind, name, _ := strings.Cut(strings.TrimSpace(v), ":")
			kind = strings.ToLower(kind)

			if name == "" || (kind != "header" && kind != "query" && kind != "cookie") {
				return nil, fmt.Errorf("jwt token_source %q is invalid", v)
			}

			if kind == "header" {
				name = http.CanonicalHeaderKey(name)
			}

			sources = append(sources, source{kind, name})
		}
	}

	claimHeaders := make(map[string]string, len(m.ClaimHeaders))
	for h, claim := range m.ClaimHeaders {
		claimHeaders[http.CanonicalHeaderKey(h)] = claim
	}

	validate := func(claims gojwt.MapClaims) error {
		if len(m.Issuer) > 0 {
			iss, _ := claims.GetIssuer()
			if !slices.Contains(m.Issuer, iss) {
				return errors.New("issuer not accepted")
			}
		}

		if len(m.Audience) > 0 {
			aud, _ := claims.GetAudience()
			if !slices.ContainsFunc(aud, func(a string) bool { return slices.Contains(m.Audience, a) }) {
				return errors.New("audience not accepted")
			}
		}

		for name, want := range m.RequiredClaims {
			v, ok := claims[name]
			if !ok {
				return fmt.Errorf("claim %q missing", name)
			}

			if want != "" && !claimContains(v, want) {
				return fmt.Errorf("claim %q not accepted", name)
			}
		}

		return nil
	}

	reject := func(w http.ResponseWriter, err error) {
		if errors.Is(err, errNoToken) {
			w.Header().Set("WWW-Authenticate", `Bearer`)
		} else {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		}

		httputil.HandleError(w, httputil.NewError("unauthorized", err, http.StatusUnauthorized))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for h := range claimHeaders {
				r.Header.Del(h)
			}

			var (
				raw  string
				from source
			)

			for _, s := range sources {
				if raw = s.get(r); raw != "" {
					from = s

					break
				}
			}

			if raw == "" {
				if m.Optional {
					next.ServeHTTP(w, r)

					return
				}

				reject(w, errNoToken)

				return
			}

			claims := gojwt.MapClaims{}
			if _, err := parser.ParseWithClaims(raw, claims, keyfunc); err != nil {
				reject(w, err)

				return
			}

			if err := validate(claims); err != nil {
				reject(w, err)

				return
			}

			// role and scope read the claims like after session
			if custom, err := toCustom(claims); err == nil {
				for _, name := range m.RolesClaim {
					for _, v := range claimValues(lookupClaim(claims, name)) {
						custom.RoleSet[v] = struct{}{}
					}
				}

				tcontext.Set(r, "claims", custom)
			}

			for h, claim := range claimHeaders {
				if v := claimString(claims[claim]); v != "" {
					r.Header.Set(h, v)
				}
			}

			if m.StripToken {
				from.strip(r)
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func claimContains(v any, want string) bool {
	switch v := v.(type) {
	case string:
		return v == want || slices.Contains(strings.Fields(v), want)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	case bool:
		return fmt.Sprint(v) == want
	case float64:
		return fmt.Sprint(v) == want
	}

	return false
}

func claimString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}

		return strings.Join(parts, ",")
	case float64, bool:
		return fmt.Sprint(v)
	}

	return ""
}

// toCustom converts the claims to the type session stores, so role and scope
// work after jwt.
func toCustom(claims gojwt.MapClaims) (*oauth2claims.Custom, error) {
	b, err := json.Marshal(claims)
	if err != nil {
		return nil, err
	}

	custom := &oauth2claims.Custom{}
	if err := json.Unmarshal(b, custom); err != nil {
		return nil, err
	}

	return custom, nil
}

// lookupClaim returns a claim by its full name, else by a dot separated path
// of nested objects.
func lookupClaim(claims gojwt.MapClaims, name string) any {
	if v, ok := claims[name]; ok {
		return v
	}

	var cur any = map[string]any(claims)

	for _, part := range strings.Split(name, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}

		if cur, ok = obj[part]; !ok {
			return nil
		}
	}

	return cur
}

// claimValues returns the items of a list claim, or of a string claim
// separated by spaces or commas.
func claimValues(v any) []string {
	switch v := v.(type) {
	case string:
		return strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == ',' })
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}

		return out
	}

	return nil
}
