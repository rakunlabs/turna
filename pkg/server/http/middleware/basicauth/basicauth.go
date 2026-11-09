package basicauth

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

const (
	basic        = "Basic"
	defaultRealm = "Restricted"
)

type BasicAuth struct {
	// Users are "username:hash" entries; bcrypt and argon2id hashes are
	// accepted.
	Users []string `cfg:"users" log:"-"`
	Realm string   `cfg:"realm"`
	// HeaderField is the name of the header field to set with the username, default X-User
	HeaderField string `cfg:"header_field"`
	// RemoveHeader removes the Authorization header from the request
	RemoveHeader bool `cfg:"remove_header"`
	// AllowLegacyHashes accepts APR1, md5crypt and unsalted {SHA} hashes.
	// They are fast to brute force; use it only while migrating.
	AllowLegacyHashes bool `cfg:"allow_legacy_hashes"`
}

func (b *BasicAuth) Middleware(name string) (func(http.Handler) http.Handler, error) {
	users := make(map[string]verifier, len(b.Users))
	for _, user := range b.Users {
		username, hash, ok := strings.Cut(user, ":")
		if !ok || username == "" || hash == "" {
			return nil, fmt.Errorf("invalid user entry, want username:hash")
		}

		v, err := newVerifier(hash, b.AllowLegacyHashes)
		if err != nil {
			return nil, fmt.Errorf("user %q: %w", username, err)
		}

		if isLegacy(hash) {
			slog.Warn("basic_auth: legacy password hash accepted, replace it with bcrypt or argon2id", "middleware", name, "user", username)
		}

		users[username] = v
	}

	realm := b.Realm
	if realm == "" {
		realm = defaultRealm
	}

	if b.HeaderField == "" {
		b.HeaderField = "X-User"
	}

	check := func(username, password string) bool {
		v, ok := users[username]
		if !ok {
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))

			return false
		}

		return v(password)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get(httputil.HeaderAuthorization)
			l := len(basic)

			if len(auth) > l+1 && strings.EqualFold(auth[:l], basic) && auth[l] == ' ' {
				// Invalid base64 shouldn't be treated as error
				// instead should be treated as invalid client input
				cred, err := base64.StdEncoding.DecodeString(strings.TrimSpace(auth[l+1:]))
				if err != nil {
					httputil.HandleError(w, httputil.NewError("", err, http.StatusBadRequest))

					return
				}

				if username, password, ok := strings.Cut(string(cred), ":"); ok && check(username, password) {
					if b.RemoveHeader {
						r.Header.Del(httputil.HeaderAuthorization)
					}

					r.Header.Set(b.HeaderField, username)

					next.ServeHTTP(w, r)

					return
				}
			}

			// Need to return `401` for browsers to pop-up login box.
			w.Header().Set(httputil.HeaderWWWAuthenticate, basic+" realm="+strconv.Quote(realm)+`, charset="UTF-8"`)
			httputil.HandleError(w, httputil.NewError("", nil, http.StatusUnauthorized))
		})
	}, nil
}
