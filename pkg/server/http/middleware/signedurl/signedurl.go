// Package signedurl lets requests pass with a valid, unexpired HMAC
// signature in the query, like nginx secure_link.
package signedurl

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type SignedURL struct {
	// Secrets verify signatures; the first one signs. More than one allows
	// rotation.
	Secrets []string `cfg:"secrets" log:"-"`

	// ExpiresParam is the query parameter with the unix expiry time,
	// default expires.
	ExpiresParam string `cfg:"expires_param"`
	// SignatureParam is the query parameter with the signature, default
	// signature.
	SignatureParam string `cfg:"signature_param"`
	// SignQuery includes the other query parameters in the signature, so
	// they cannot be changed. Default true.
	SignQuery *bool `cfg:"sign_query"`
	// StripParams removes the expiry and signature from the query sent
	// upstream. Default true.
	StripParams *bool `cfg:"strip_params"`
	// PathPrefix signs only this prefix of the path, so one link opens
	// everything below it, e.g. a folder.
	PathPrefix bool `cfg:"path_prefix"`

	// MaxTTL rejects links that expire further in the future, 0 is no limit.
	MaxTTL time.Duration `cfg:"max_ttl"`

	// SignPath answers requests to this path with a signed URL of the
	// "path" query parameter, valid for the "ttl" query parameter or
	// DefaultTTL. Protect it with an auth middleware in front.
	SignPath string `cfg:"sign_path"`
	// DefaultTTL of URLs from SignPath, default 1h.
	DefaultTTL time.Duration `cfg:"default_ttl"`

	// StatusCode of rejected requests, default 403.
	StatusCode int `cfg:"status_code"`
}

type signer struct {
	secrets    [][]byte
	expires    string
	signature  string
	signQuery  bool
	strip      bool
	pathPrefix bool
	maxTTL     time.Duration
	signPath   string
	defaultTTL time.Duration
	statusCode int
	now        func() time.Time
}

func (m *SignedURL) build() (*signer, error) {
	if len(m.Secrets) == 0 {
		return nil, errors.New("signed_url secrets are required")
	}

	s := &signer{
		expires:    defaultString(m.ExpiresParam, "expires"),
		signature:  defaultString(m.SignatureParam, "signature"),
		signQuery:  m.SignQuery == nil || *m.SignQuery,
		strip:      m.StripParams == nil || *m.StripParams,
		pathPrefix: m.PathPrefix,
		maxTTL:     m.MaxTTL,
		signPath:   m.SignPath,
		defaultTTL: m.DefaultTTL,
		statusCode: m.StatusCode,
		now:        time.Now,
	}

	for _, secret := range m.Secrets {
		if len(secret) < 16 {
			return nil, errors.New("signed_url secrets must be at least 16 bytes")
		}

		s.secrets = append(s.secrets, []byte(secret))
	}

	if s.expires == s.signature {
		return nil, errors.New("signed_url expires_param and signature_param must differ")
	}

	if s.defaultTTL == 0 {
		s.defaultTTL = time.Hour
	}

	if s.maxTTL < 0 || s.defaultTTL < 0 {
		return nil, errors.New("signed_url durations must not be negative")
	}

	if s.maxTTL > 0 && s.defaultTTL > s.maxTTL {
		s.defaultTTL = s.maxTTL
	}

	if s.statusCode == 0 {
		s.statusCode = http.StatusForbidden
	}

	return s, nil
}

func defaultString(v, d string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}

	return d
}

// payload is what gets signed: the expiry, the path and the other query
// parameters in their canonical order.
func (s *signer) payload(path string, query url.Values, expires string) string {
	var b strings.Builder

	b.WriteString(expires)
	b.WriteByte('\n')
	b.WriteString(path)
	b.WriteByte('\n')

	if s.signQuery {
		q := url.Values{}
		for k, v := range query {
			if k != s.expires && k != s.signature {
				q[k] = v
			}
		}

		b.WriteString(q.Encode())
	}

	return b.String()
}

func mac(secret []byte, payload string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(payload))

	return h.Sum(nil)
}

// Sign returns path with the expiry and signature added to its query.
func (s *signer) Sign(rawURL string, ttl time.Duration) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(u.Path, "/") {
		return "", errors.New("path must start with /")
	}

	u.Path = cleanPath(u.Path)
	u.RawPath = ""

	query := u.Query()
	expires := strconv.FormatInt(s.now().Add(ttl).Unix(), 10)

	query.Del(s.signature)
	query.Set(s.expires, expires)

	sig := base64.RawURLEncoding.EncodeToString(mac(s.secrets[0], s.payload(u.Path, query, expires)))
	query.Set(s.signature, sig)

	u.RawQuery = query.Encode()

	return u.String(), nil
}

func (s *signer) verify(r *http.Request) error {
	query := r.URL.Query()

	expires := query.Get(s.expires)
	sigParam := query.Get(s.signature)
	if expires == "" || sigParam == "" {
		return errors.New("signature missing")
	}

	exp, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return errors.New("expiry is invalid")
	}

	now := s.now()
	until := time.Unix(exp, 0)

	if !now.Before(until) {
		return errors.New("link expired")
	}

	if s.maxTTL > 0 && until.Sub(now) > s.maxTTL {
		return errors.New("link expires too late")
	}

	sig, err := base64.RawURLEncoding.DecodeString(sigParam)
	if err != nil {
		return errors.New("signature is invalid")
	}

	p := cleanPath(r.URL.Path)

	paths := []string{p}
	if s.pathPrefix {
		// a link signed for /files/ opens everything below it
		paths = prefixes(p)
	}

	for _, p := range paths {
		payload := s.payload(p, query, expires)
		for _, secret := range s.secrets {
			if hmac.Equal(sig, mac(secret, payload)) {
				return nil
			}
		}
	}

	return errors.New("signature is invalid")
}

// cleanPath resolves "." and ".." like file servers do, so a link signed for
// /files/ cannot open /files/../secret. A trailing slash is kept.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}

	c := path.Clean("/" + p)
	if strings.HasSuffix(p, "/") && c != "/" {
		c += "/"
	}

	return c
}

// prefixes returns path and each of its parent directories ending in "/".
func prefixes(path string) []string {
	out := []string{path}

	for i := len(path) - 1; i > 0; i-- {
		if path[i-1] == '/' {
			out = append(out, path[:i])
		}
	}

	return out
}

func (m *SignedURL) Middleware() (func(http.Handler) http.Handler, error) {
	s, err := m.build()
	if err != nil {
		return nil, err
	}

	return s.middleware, nil
}

func (s *signer) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.signPath != "" && r.URL.Path == s.signPath {
			s.serveSign(w, r)

			return
		}

		if err := s.verify(r); err != nil {
			httputil.HandleError(w, httputil.NewError(err.Error(), nil, s.statusCode))

			return
		}

		// the upstream must serve the path that was verified
		if c := cleanPath(r.URL.Path); c != r.URL.Path {
			r.URL.Path = c
			r.URL.RawPath = ""
			r.RequestURI = r.URL.RequestURI()
		}

		if s.strip {
			query := r.URL.Query()
			query.Del(s.expires)
			query.Del(s.signature)

			r.URL.RawQuery = query.Encode()
			r.RequestURI = r.URL.RequestURI()
		}

		next.ServeHTTP(w, r)
	})
}

func (s *signer) serveSign(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	path := query.Get("path")
	if path == "" {
		httputil.HandleError(w, httputil.NewError("path query parameter is required", nil, http.StatusBadRequest))

		return
	}

	ttl := s.defaultTTL
	if v := query.Get("ttl"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			httputil.HandleError(w, httputil.NewError("ttl is invalid", err, http.StatusBadRequest))

			return
		}

		ttl = d
	}

	if s.maxTTL > 0 && ttl > s.maxTTL {
		httputil.HandleError(w, httputil.NewError(fmt.Sprintf("ttl exceeds %s", s.maxTTL), nil, http.StatusBadRequest))

		return
	}

	signed, err := s.Sign(path, ttl)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("path is invalid", err, http.StatusBadRequest))

		return
	}

	_ = httputil.JSON(w, http.StatusOK, map[string]any{
		"url":     signed,
		"expires": s.now().Add(ttl).Unix(),
	})
}
