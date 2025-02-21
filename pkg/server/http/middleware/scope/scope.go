package scope

import (
	"net/http"
	"strings"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/tcontext"
)

type Scope struct {
	Scopes  []string `cfg:"scopes"`
	Methods []string `cfg:"methods"`

	Noop bool `cfg:"noop"`
}

var (
	DisableScopeCheckKey = "auth_disable_scope_check"
	KeyAuthNoop          = "auth_noop"
	KeyClaims            = "claims"
)

type ClaimsScope interface {
	HasScope(scope string) bool
}

// MiddlewareScope that checks the scope claim.
//
// This middleware just work with ClaimsScope interface in claims.
func (m *Scope) Middleware() func(http.Handler) http.Handler {
	methodSet := make(map[string]struct{}, len(m.Methods))
	for _, method := range m.Methods {
		methodSet[strings.ToUpper(method)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.Noop {
				next.ServeHTTP(w, r)

				return
			}

			turna, ok := tcontext.GetTurna(r)
			if !ok {
				httputil.HandleError(w, httputil.NewError("turna not found", nil, http.StatusInternalServerError))

				return
			}

			if v, ok := turna.GetInterface(DisableScopeCheckKey).(bool); ok && v {
				next.ServeHTTP(w, r)

				return
			}

			if v, ok := turna.GetInterface(KeyAuthNoop).(bool); ok && v {
				next.ServeHTTP(w, r)

				return
			}

			if len(methodSet) > 0 {
				if _, ok := methodSet[r.Method]; !ok {
					next.ServeHTTP(w, r)

					return
				}
			}

			claimsV, ok := turna.GetInterface(KeyClaims).(ClaimsScope)
			if !ok {
				httputil.HandleError(w, httputil.NewError("claims not found", nil, http.StatusUnauthorized))

				return
			}

			if len(m.Scopes) > 0 {
				found := false
				for _, scope := range m.Scopes {
					if claimsV.HasScope(scope) {
						found = true

						break
					}
				}

				if !found {
					httputil.HandleError(w, httputil.NewError("scope not authorized", nil, http.StatusUnauthorized))

					return
				}
			}

			next.ServeHTTP(w, r)

			return
		})
	}
}
