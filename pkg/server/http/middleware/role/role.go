package role

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

var (
	DisableRoleCheckKey = "auth_disable_role_check"
	KeyAuthNoop         = "auth_noop"
	KeyClaims           = "claims"

	ReadMethods = map[string]struct{}{
		"GET":     {},
		"HEAD":    {},
		"OPTIONS": {},
		"TRACE":   {},
		"CONNECT": {},
	}
	WriteMethods = map[string]struct{}{
		"POST":   {},
		"PUT":    {},
		"PATCH":  {},
		"DELETE": {},
	}
)

// Role authorizes requests with roles from the claims stored by session.
//
// Checks run in this order: global roles/methods, then path_map rules.
// When data is set, the middleware responds with role based data instead of
// calling the next handler.
type Role struct {
	// Roles is a global requirement, any one listed role is enough.
	Roles []string `cfg:"roles"`
	// Methods restricts the global roles check to these methods.
	Methods []string `cfg:"methods"`

	PathMap []PathMap `cfg:"path_map"`
	// AllowOthers allows paths that do not match any path_map entry.
	AllowOthers bool `cfg:"allow_others"`

	Redirect Redirect `cfg:"redirect"`

	Data *Data `cfg:"data"`

	Noop bool `cfg:"noop"`

	methods map[string]struct{}
}

type Redirect struct {
	Enable bool   `cfg:"enable"`
	URL    string `cfg:"url"`
}

type PathMap struct {
	RegexPath string `cfg:"regex_path"`
	Map       []Map  `cfg:"map"`

	regexPath *regexp.Regexp
}

type Map struct {
	AllMethods    bool     `cfg:"all_methods"`
	ReadMethods   bool     `cfg:"read_methods"`
	WriteMethods  bool     `cfg:"write_methods"`
	Methods       []string `cfg:"methods"`
	Roles         []string `cfg:"roles"`
	RolesDisabled bool     `cfg:"roles_disabled"`

	methods map[string]struct{}
}

type Data struct {
	Map     []DataMap `cfg:"map"`
	Default any       `cfg:"default"`
}

type DataMap struct {
	Roles []string `cfg:"roles"`
	Data  any      `cfg:"data"`
}

type ClaimsRole interface {
	HasRole(role string) bool
}

func methodSet(methods []string) map[string]struct{} {
	set := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		set[strings.ToUpper(method)] = struct{}{}
	}

	return set
}

func hasAnyRole(claims ClaimsRole, roles []string) bool {
	if claims == nil {
		return false
	}

	for _, role := range roles {
		if claims.HasRole(role) {
			return true
		}
	}

	return false
}

func (m *Map) matchMethod(method string) bool {
	if m.AllMethods {
		return true
	}

	if m.ReadMethods {
		if _, ok := ReadMethods[method]; ok {
			return true
		}
	}

	if m.WriteMethods {
		if _, ok := WriteMethods[method]; ok {
			return true
		}
	}

	_, ok := m.methods[method]

	return ok
}

func (m *Role) Middleware() (func(http.Handler) http.Handler, error) {
	m.methods = methodSet(m.Methods)

	for i := range m.PathMap {
		regexPath, err := regexp.Compile(m.PathMap[i].RegexPath)
		if err != nil {
			return nil, fmt.Errorf("invalid regex path %q: %w", m.PathMap[i].RegexPath, err)
		}

		m.PathMap[i].regexPath = regexPath

		for j := range m.PathMap[i].Map {
			m.PathMap[i].Map[j].methods = methodSet(m.PathMap[i].Map[j].Methods)
		}
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

			// claims may be missing when session let an anonymous request
			// through on skip_paths or auth_skip_paths.
			claims, _ := turna.GetInterface(KeyClaims).(ClaimsRole)

			if !m.skipCheck(turna) {
				if allowed := m.allowed(claims, r); !allowed {
					m.deny(w, claims)

					return
				}
			}

			if m.Data != nil {
				httputil.JSON(w, http.StatusOK, m.Data.values(claims))

				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func (m *Role) skipCheck(turna *tcontext.Turna) bool {
	for _, key := range []string{DisableRoleCheckKey, KeyAuthNoop, session.CtxPublicAccessKey} {
		if v, ok := turna.GetInterface(key).(bool); ok && v {
			return true
		}
	}

	return false
}

func (m *Role) allowed(claims ClaimsRole, r *http.Request) bool {
	method := strings.ToUpper(r.Method)

	if len(m.Roles) > 0 {
		_, inMethods := m.methods[method]
		if len(m.methods) == 0 || inMethods {
			if !hasAnyRole(claims, m.Roles) {
				return false
			}
		}
	}

	if len(m.PathMap) == 0 {
		return true
	}

	for _, pathMap := range m.PathMap {
		if !pathMap.regexPath.MatchString(r.URL.Path) {
			continue
		}

		for _, rule := range pathMap.Map {
			if !rule.matchMethod(method) {
				continue
			}

			if rule.RolesDisabled || hasAnyRole(claims, rule.Roles) {
				return true
			}
		}

		return false
	}

	return m.AllowOthers
}

func (m *Role) deny(w http.ResponseWriter, claims ClaimsRole) {
	if m.Redirect.Enable {
		httputil.Redirect(w, http.StatusTemporaryRedirect, m.Redirect.URL)

		return
	}

	if claims == nil {
		httputil.HandleError(w, httputil.NewError("claims not found", nil, http.StatusUnauthorized))

		return
	}

	httputil.HandleError(w, httputil.NewError("role not authorized", nil, http.StatusForbidden))
}

func (d *Data) values(claims ClaimsRole) []any {
	values := []any{}
	for _, data := range d.Map {
		if hasAnyRole(claims, data.Roles) {
			values = append(values, data.Data)
		}
	}

	if d.Default != nil {
		switch v := d.Default.(type) {
		case []any:
			values = append(values, v...)
		default:
			values = append(values, v)
		}
	}

	return values
}
