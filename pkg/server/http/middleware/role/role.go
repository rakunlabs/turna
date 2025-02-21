package role

import (
	"net/http"
	"strings"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/tcontext"
)

var (
	DisableRoleCheckKey = "auth_disable_role_check"
	KeyAuthNoop         = "auth_noop"
	KeyClaims           = "claims"
)

type Role struct {
	Roles   []string `cfg:"roles"`
	Methods []string `cfg:"methods"`

	Noop bool `cfg:"noop"`
}

type ClaimsRole interface {
	HasRole(role string) bool
}

// MiddlewareRole that checks the role claim.
//
// This middleware just work with ClaimsRole interface in claim.
func (m *Role) Middleware() func(http.Handler) http.Handler {
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

			if v, ok := turna.GetInterface(DisableRoleCheckKey).(bool); ok && v {
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

			claimsV, ok := turna.GetInterface(KeyClaims).(ClaimsRole)
			if !ok {
				httputil.HandleError(w, httputil.NewError("claims not found", nil, http.StatusUnauthorized))

				return
			}

			if len(m.Roles) > 0 {
				found := false
				for _, role := range m.Roles {
					if claimsV.HasRole(role) {
						found = true

						break
					}
				}

				if !found {
					httputil.HandleError(w, httputil.NewError("role not authorized", nil, http.StatusUnauthorized))

					return
				}
			}

			next.ServeHTTP(w, r)

			return
		})
	}
}
