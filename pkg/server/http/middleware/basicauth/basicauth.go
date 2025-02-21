package basicauth

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	goauth "github.com/abbot/go-http-auth"
	"github.com/worldline-go/turna/pkg/server/http/httputil"
)

const (
	basic        = "Basic"
	defaultRealm = "Restricted"
)

// BasicAuth mostly copied from echo's BasicAuth middleware.
type BasicAuth struct {
	Users []string `cfg:"users"`
	Realm string   `cfg:"realm"`
	// HeaderField is the name of the header field to set with the username, default X-User
	HeaderField string `cfg:"header_field"`
	// RemoveHeader removes the Authorization header from the request
	RemoveHeader bool `cfg:"remove_header"`
}

func (b *BasicAuth) Middleware(name string) (func(http.Handler) http.Handler, error) {
	v := make(map[string]string, len(b.Users))
	for _, user := range b.Users {
		parts := strings.Split(user, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid user: %q", user)
		}

		v[parts[0]] = parts[1]
	}

	realm := b.Realm
	if realm == "" {
		realm = defaultRealm
	}

	gAuth := goauth.BasicAuth{
		Realm: realm,
		Secrets: func(user, realm string) string {
			if hash, ok := v[user]; ok {
				return hash
			}

			return ""
		},
	}

	if b.HeaderField == "" {
		b.HeaderField = "X-User"
	}

	validator := func(username, password string, r *http.Request) bool {
		if secret := gAuth.Secrets(username, realm); secret == "" || !goauth.CheckSecret(password, secret) {
			return false
		}

		if b.RemoveHeader {
			r.Header.Del("Authorization")
		}

		if b.HeaderField != "" {
			r.Header.Set(b.HeaderField, username)
		}

		return true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get(httputil.HeaderAuthorization)
			l := len(basic)

			if len(auth) > l+1 && strings.EqualFold(auth[:l], basic) {
				// Invalid base64 shouldn't be treated as error
				// instead should be treated as invalid client input
				b, err := base64.StdEncoding.DecodeString(auth[l+1:])
				if err != nil {
					httputil.HandleError(w, httputil.NewError("", err, http.StatusBadRequest))

					return
				}

				cred := string(b)

				for i := range len(cred) {
					if cred[i] == ':' {
						// Verify credentials
						valid := validator(cred[:i], cred[i+1:], r)
						if valid {
							next.ServeHTTP(w, r)

							return
						}

						break
					}
				}
			}

			// Need to return `401` for browsers to pop-up login box.
			w.Header().Set(httputil.HeaderWWWAuthenticate, basic+" realm="+strconv.Quote(realm))
			httputil.HandleError(w, httputil.NewError("", nil, http.StatusUnauthorized))
		})
	}, nil
}
