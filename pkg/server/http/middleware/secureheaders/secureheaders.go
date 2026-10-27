// Package secureheaders sets security related response headers with safe
// defaults.
package secureheaders

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const nonceHolder = "{nonce}"

type SecureHeaders struct {
	// HSTS sets Strict-Transport-Security on HTTPS requests.
	HSTS *HSTS `cfg:"hsts"`
	// ContentSecurityPolicy is the CSP value; {nonce} is replaced with a
	// random value per request.
	ContentSecurityPolicy string `cfg:"content_security_policy"`
	// ContentSecurityPolicyReportOnly sends the policy as
	// Content-Security-Policy-Report-Only.
	ContentSecurityPolicyReportOnly bool `cfg:"content_security_policy_report_only"`
	// NonceHeader passes the nonce to the next handlers in this request
	// header, e.g. X-CSP-Nonce.
	NonceHeader string `cfg:"nonce_header"`

	// FrameOptions is X-Frame-Options, default DENY; "-" disables it.
	FrameOptions string `cfg:"frame_options"`
	// ContentTypeOptions is X-Content-Type-Options, default nosniff; "-"
	// disables it.
	ContentTypeOptions string `cfg:"content_type_options"`
	// ReferrerPolicy is Referrer-Policy, default
	// strict-origin-when-cross-origin; "-" disables it.
	ReferrerPolicy string `cfg:"referrer_policy"`
	// PermissionsPolicy is Permissions-Policy.
	PermissionsPolicy string `cfg:"permissions_policy"`
	// CrossOriginOpenerPolicy is Cross-Origin-Opener-Policy.
	CrossOriginOpenerPolicy string `cfg:"cross_origin_opener_policy"`
	// CrossOriginResourcePolicy is Cross-Origin-Resource-Policy.
	CrossOriginResourcePolicy string `cfg:"cross_origin_resource_policy"`
	// CrossOriginEmbedderPolicy is Cross-Origin-Embedder-Policy.
	CrossOriginEmbedderPolicy string `cfg:"cross_origin_embedder_policy"`

	// Override replaces headers already set by later handlers or the
	// upstream, default keeps them.
	Override bool `cfg:"override"`
	// RemoveHeaders are response headers to remove, e.g. Server or
	// X-Powered-By.
	RemoveHeaders []string `cfg:"remove_headers"`
}

type HSTS struct {
	// MaxAge default is 1 year.
	MaxAge            time.Duration `cfg:"max_age"`
	IncludeSubdomains bool          `cfg:"include_subdomains"`
	Preload           bool          `cfg:"preload"`
	// TrustForwardedProto also sends the header when X-Forwarded-Proto is
	// https, for TLS terminated in front of Turna.
	TrustForwardedProto bool `cfg:"trust_forwarded_proto"`
}

type header struct {
	name, value string
}

func value(v, def string) string {
	switch v = strings.TrimSpace(v); v {
	case "-":
		return ""
	case "":
		return def
	}

	return v
}

func (m *SecureHeaders) Middleware() (func(http.Handler) http.Handler, error) {
	var static []header

	add := func(name, v string) {
		if v != "" {
			static = append(static, header{name, v})
		}
	}

	add("X-Frame-Options", value(m.FrameOptions, "DENY"))
	add("X-Content-Type-Options", value(m.ContentTypeOptions, "nosniff"))
	add("Referrer-Policy", value(m.ReferrerPolicy, "strict-origin-when-cross-origin"))
	add("Permissions-Policy", value(m.PermissionsPolicy, ""))
	add("Cross-Origin-Opener-Policy", value(m.CrossOriginOpenerPolicy, ""))
	add("Cross-Origin-Resource-Policy", value(m.CrossOriginResourcePolicy, ""))
	add("Cross-Origin-Embedder-Policy", value(m.CrossOriginEmbedderPolicy, ""))

	csp := strings.TrimSpace(m.ContentSecurityPolicy)
	cspName := "Content-Security-Policy"
	if m.ContentSecurityPolicyReportOnly {
		cspName = "Content-Security-Policy-Report-Only"
	}

	useNonce := strings.Contains(csp, nonceHolder)
	nonceHeader := strings.TrimSpace(m.NonceHeader)
	if nonceHeader != "" && !useNonce {
		return nil, errors.New("secure_headers nonce_header needs {nonce} in content_security_policy")
	}

	var hsts string
	if m.HSTS != nil {
		if m.HSTS.MaxAge < 0 {
			return nil, errors.New("secure_headers hsts max_age must not be negative")
		}

		maxAge := m.HSTS.MaxAge
		if maxAge == 0 {
			maxAge = 365 * 24 * time.Hour
		}

		hsts = "max-age=" + strconv.FormatInt(int64(maxAge/time.Second), 10)
		if m.HSTS.IncludeSubdomains {
			hsts += "; includeSubDomains"
		}
		if m.HSTS.Preload {
			if !m.HSTS.IncludeSubdomains || maxAge < 365*24*time.Hour {
				return nil, errors.New("secure_headers hsts preload needs include_subdomains and max_age of at least 1 year")
			}

			hsts += "; preload"
		}
	}

	remove := make([]string, 0, len(m.RemoveHeaders))
	for _, h := range m.RemoveHeaders {
		if h = strings.TrimSpace(h); h != "" {
			remove = append(remove, h)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers := make([]header, len(static), len(static)+2)
			copy(headers, static)

			if csp != "" {
				v := csp
				if useNonce {
					nonce, err := newNonce()
					if err != nil {
						http.Error(w, "nonce generation failed", http.StatusInternalServerError)

						return
					}

					v = strings.ReplaceAll(csp, nonceHolder, nonce)

					if nonceHeader != "" {
						r.Header.Set(nonceHeader, nonce)
					}
				}

				headers = append(headers, header{cspName, v})
			}

			if hsts != "" && (r.TLS != nil ||
				(m.HSTS.TrustForwardedProto && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))) {
				headers = append(headers, header{"Strict-Transport-Security", hsts})
			}

			rw := &responseWriter{ResponseWriter: w, apply: func(h http.Header) {
				for _, name := range remove {
					h.Del(name)
				}

				for _, hd := range headers {
					if m.Override || h.Get(hd.name) == "" {
						h.Set(hd.name, hd.value)
					}
				}
			}}

			next.ServeHTTP(rw, r)

			rw.applyOnce()
		})
	}, nil
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secure_headers nonce: %w", err)
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

// responseWriter applies the headers when the response starts, so headers of
// later handlers and upstreams are seen.
type responseWriter struct {
	http.ResponseWriter

	apply   func(http.Header)
	applied bool
}

func (w *responseWriter) applyOnce() {
	if w.applied {
		return
	}

	w.applied = true
	w.apply(w.ResponseWriter.Header())
}

func (w *responseWriter) WriteHeader(code int) {
	// 1xx responses are followed by the final one, 101 is final
	if code < 100 || code >= 200 || code == http.StatusSwitchingProtocols {
		w.applyOnce()
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.applyOnce()

	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) Flush() {
	w.applyOnce()

	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
