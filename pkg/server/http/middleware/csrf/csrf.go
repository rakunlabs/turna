// Package csrf protects unsafe requests against cross-site request forgery.
//
// The origin check uses the Sec-Fetch-Site and Origin headers
// (net/http.CrossOriginProtection). The optional token check implements the
// signed double-submit cookie pattern for clients that need an explicit token.
package csrf

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type CSRF struct {
	// TrustedOrigins are allowed cross-origin sources, e.g. "https://app.example.com".
	TrustedOrigins []string `cfg:"trusted_origins"`
	// BypassPatterns are ServeMux patterns ("POST /webhook/{id}", "/api/") skipping all checks.
	BypassPatterns []string `cfg:"bypass_patterns"`
	// DisableOriginCheck disables the Sec-Fetch-Site/Origin check.
	DisableOriginCheck bool `cfg:"disable_origin_check"`

	Token Token `cfg:"token"`
}

type Token struct {
	Enabled bool `cfg:"enabled"`
	// Secret signs the token; without it the token is not signed, which is
	// weaker against cookie injection from subdomains.
	Secret string `cfg:"secret" log:"-"`

	// CookieName default is "csrf_token".
	CookieName string `cfg:"cookie_name"`
	// HeaderName default is "X-CSRF-Token".
	HeaderName string `cfg:"header_name"`
	// FormField is checked for form posts when the header is empty, default is "csrf_token".
	FormField string `cfg:"form_field"`

	CookiePath   string        `cfg:"cookie_path"`
	CookieDomain string        `cfg:"cookie_domain"`
	CookieMaxAge time.Duration `cfg:"cookie_max_age"`
	// CookieSecure default is true.
	CookieSecure *bool `cfg:"cookie_secure"`
	// CookieSameSite is lax, strict or none; default is lax.
	CookieSameSite string `cfg:"cookie_same_site"`
}

const tokenSize = 32

var (
	errTokenMissing = errors.New("csrf token missing")
	errTokenInvalid = errors.New("csrf token invalid")
)

func (m *CSRF) Middleware() (func(http.Handler) http.Handler, error) {
	cop := http.NewCrossOriginProtection()

	for _, origin := range m.TrustedOrigins {
		if err := cop.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("trusted origin %q: %w", origin, err)
		}
	}

	bypass := http.NewServeMux()
	for _, pattern := range m.BypassPatterns {
		if err := registerPattern(bypass, pattern); err != nil {
			return nil, err
		}
	}

	tokenCheck, err := m.Token.checker()
	if err != nil {
		return nil, err
	}

	deny := func(w http.ResponseWriter, err error) {
		httputil.HandleError(w, httputil.NewError("cross-site request rejected", err, http.StatusForbidden))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(m.BypassPatterns) > 0 {
				if _, pattern := bypass.Handler(r); pattern != "" {
					next.ServeHTTP(w, r)

					return
				}
			}

			if !m.DisableOriginCheck {
				if err := cop.Check(r); err != nil {
					deny(w, err)

					return
				}
			}

			if tokenCheck != nil {
				if err := tokenCheck(w, r); err != nil {
					deny(w, err)

					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func registerPattern(mux *http.ServeMux, pattern string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("bypass pattern %q: %v", pattern, r)
		}
	}()

	mux.Handle(pattern, http.NotFoundHandler())

	return nil
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}

	return false
}

func (t *Token) checker() (func(w http.ResponseWriter, r *http.Request) error, error) {
	if !t.Enabled {
		return nil, nil
	}

	cookieName := t.CookieName
	if cookieName == "" {
		cookieName = "csrf_token"
	}

	headerName := t.HeaderName
	if headerName == "" {
		headerName = "X-CSRF-Token"
	}

	formField := t.FormField
	if formField == "" {
		formField = "csrf_token"
	}

	cookiePath := t.CookiePath
	if cookiePath == "" {
		cookiePath = "/"
	}

	secure := true
	if t.CookieSecure != nil {
		secure = *t.CookieSecure
	}

	var sameSite http.SameSite
	switch strings.ToLower(t.CookieSameSite) {
	case "", "lax":
		sameSite = http.SameSiteLaxMode
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		sameSite = http.SameSiteNoneMode
		if !secure {
			return nil, errors.New("cookie_same_site none requires cookie_secure")
		}
	default:
		return nil, fmt.Errorf("unsupported cookie_same_site %q", t.CookieSameSite)
	}

	signer := tokenSigner{secret: []byte(t.Secret)}

	setCookie := func(w http.ResponseWriter) error {
		token, err := signer.generate()
		if err != nil {
			return err
		}

		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    token,
			Path:     cookiePath,
			Domain:   t.CookieDomain,
			MaxAge:   int(t.CookieMaxAge.Seconds()),
			Secure:   secure,
			HttpOnly: false, // JavaScript must read it to send it back in the header
			SameSite: sameSite,
		})

		return nil
	}

	return func(w http.ResponseWriter, r *http.Request) error {
		var cookieToken string
		if c, err := r.Cookie(cookieName); err == nil && signer.valid(c.Value) {
			cookieToken = c.Value
		}

		if isSafeMethod(r.Method) {
			if cookieToken == "" {
				return setCookie(w)
			}

			return nil
		}

		if cookieToken == "" {
			return errTokenMissing
		}

		sent := r.Header.Get(headerName)
		if sent == "" && isForm(r) {
			sent = r.PostFormValue(formField)
		}

		if sent == "" {
			return errTokenMissing
		}

		if subtle.ConstantTimeCompare([]byte(sent), []byte(cookieToken)) != 1 {
			return errTokenInvalid
		}

		return nil
	}, nil
}

func isForm(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")

	return strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data")
}

type tokenSigner struct {
	secret []byte
}

func (s tokenSigner) generate() (string, error) {
	b := make([]byte, tokenSize)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate csrf token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(b)
	if len(s.secret) == 0 {
		return token, nil
	}

	return token + "." + s.sign(token), nil
}

func (s tokenSigner) sign(token string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(token))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s tokenSigner) valid(value string) bool {
	if len(s.secret) == 0 {
		b, err := base64.RawURLEncoding.DecodeString(value)

		return err == nil && len(b) == tokenSize
	}

	token, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}

	return hmac.Equal([]byte(sig), []byte(s.sign(token)))
}
