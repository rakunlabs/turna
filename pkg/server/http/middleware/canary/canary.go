// Package canary splits traffic between middleware chains by weight, with
// sticky assignment and overrides by header or cookie.
package canary

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/requestkey"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

type Canary struct {
	// Variants are the chains traffic is split between.
	Variants []Variant `cfg:"variants"`

	// Sticky keeps a client on the same variant.
	Sticky *Sticky `cfg:"sticky"`
	// OverrideHeader selects a variant by name, e.g. X-Canary: beta.
	OverrideHeader string `cfg:"override_header"`
	// OverrideCookie selects a variant by name from a cookie.
	OverrideCookie string `cfg:"override_cookie"`
	// VariantHeader is set on the request with the selected variant name,
	// e.g. for logs or the upstream.
	VariantHeader string `cfg:"variant_header"`
}

type Variant struct {
	Name string `cfg:"name"`
	// Weight of the variant, relative to the others.
	Weight int `cfg:"weight"`
	// Middlewares handle the requests of the variant, usually ending with a
	// service.
	Middlewares []string `cfg:"middlewares"`
}

type Sticky struct {
	// Key hashes these request parts to pick the variant: ip, header:<name>,
	// cookie:<name> and others like rate_limit. Default is a cookie set by
	// the middleware.
	Key []string `cfg:"key"`
	// TrustedProxies are CIDRs whose forwarding headers are used for the ip key.
	TrustedProxies []string `cfg:"trusted_proxies"`

	// Cookie name of the assigned variant, default turna_canary.
	Cookie string `cfg:"cookie"`
	// MaxAge of the cookie, default 24h.
	MaxAge time.Duration `cfg:"max_age"`
	Path   string        `cfg:"path"`
	Domain string        `cfg:"domain"`
	Secure bool          `cfg:"secure"`
}

// Refs returns the names of the middlewares the variants use.
func (m *Canary) Refs() []string {
	var refs []string
	for _, v := range m.Variants {
		refs = append(refs, v.Middlewares...)
	}

	return refs
}

type variant struct {
	name    string
	weight  int
	handler http.Handler
}

func (m *Canary) Middleware() (func(http.Handler) http.Handler, error) {
	if len(m.Variants) < 2 {
		return nil, errors.New("canary needs at least two variants")
	}

	type built struct {
		name   string
		weight int
		chain  []func(http.Handler) http.Handler
	}

	var (
		list  []built
		total int
		names = map[string]bool{}
	)

	for i, v := range m.Variants {
		if v.Name == "" {
			return nil, fmt.Errorf("canary variant %d has no name", i)
		}

		if names[v.Name] {
			return nil, fmt.Errorf("canary variant %q is duplicated", v.Name)
		}
		names[v.Name] = true

		if v.Weight < 0 {
			return nil, fmt.Errorf("canary variant %q weight must not be negative", v.Name)
		}

		if len(v.Middlewares) == 0 {
			return nil, fmt.Errorf("canary variant %q has no middlewares", v.Name)
		}

		var chain []func(http.Handler) http.Handler
		for _, name := range v.Middlewares {
			mw, err := registry.GlobalReg.GetHttpMiddleware(name)
			if err != nil {
				return nil, fmt.Errorf("canary variant %q: %w", v.Name, err)
			}

			chain = append(chain, mw...)
		}

		list = append(list, built{v.Name, v.Weight, chain})
		total += v.Weight
	}

	if total == 0 {
		return nil, errors.New("canary needs a variant with weight")
	}

	var (
		stickyKey func(*http.Request) string
		cookie    *Sticky
	)

	if m.Sticky != nil {
		if len(m.Sticky.Key) > 0 {
			kf, err := requestkey.New("canary sticky", m.Sticky.Key, m.Sticky.TrustedProxies)
			if err != nil {
				return nil, err
			}

			stickyKey = kf
		} else {
			c := *m.Sticky
			cookie = &c
			if cookie.Cookie == "" {
				cookie.Cookie = "turna_canary"
			}
			if cookie.MaxAge == 0 {
				cookie.MaxAge = 24 * time.Hour
			}
			if cookie.MaxAge < 0 {
				return nil, errors.New("canary sticky max_age must not be negative")
			}
			if cookie.Path == "" {
				cookie.Path = "/"
			}
		}
	}

	variantHeader := http.CanonicalHeaderKey(strings.TrimSpace(m.VariantHeader))

	return func(next http.Handler) http.Handler {
		variants := make([]variant, len(list))
		byName := make(map[string]*variant, len(list))

		for i, b := range list {
			var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// a variant without a terminal middleware continues the route
				if next != nil {
					next.ServeHTTP(w, r)

					return
				}

				httputil.HandleError(w, httputil.NewError("no handler", nil, http.StatusNotFound))
			})

			for j := len(b.chain) - 1; j >= 0; j-- {
				h = b.chain[j](h)
			}

			variants[i] = variant{b.name, b.weight, h}
			byName[b.name] = &variants[i]
		}

		pick := func(n uint64) *variant {
			n %= uint64(total)
			for i := range variants {
				if n < uint64(variants[i].weight) {
					return &variants[i]
				}

				n -= uint64(variants[i].weight)
			}

			return &variants[len(variants)-1]
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var v *variant

			if m.OverrideHeader != "" {
				v = byName[r.Header.Get(m.OverrideHeader)]
			}

			if v == nil && m.OverrideCookie != "" {
				if c, err := r.Cookie(m.OverrideCookie); err == nil {
					v = byName[c.Value]
				}
			}

			if v == nil {
				switch {
				case stickyKey != nil:
					h := fnv.New64a()
					_, _ = h.Write([]byte(stickyKey(r)))
					v = pick(h.Sum64())
				case cookie != nil:
					if c, err := r.Cookie(cookie.Cookie); err == nil {
						v = byName[c.Value]
						// a variant whose weight dropped to 0 is drained
						if v != nil && v.weight == 0 {
							v = nil
						}
					}

					if v == nil {
						v = pick(rand.Uint64())
						http.SetCookie(w, &http.Cookie{
							Name:     cookie.Cookie,
							Value:    v.name,
							Path:     cookie.Path,
							Domain:   cookie.Domain,
							MaxAge:   int(cookie.MaxAge / time.Second),
							Secure:   cookie.Secure,
							HttpOnly: true,
							SameSite: http.SameSiteLaxMode,
						})
					}
				default:
					v = pick(rand.Uint64())
				}
			}

			if variantHeader != "" {
				r.Header.Set(variantHeader, v.name)
			}

			v.handler.ServeHTTP(w, r)
		})
	}, nil
}
