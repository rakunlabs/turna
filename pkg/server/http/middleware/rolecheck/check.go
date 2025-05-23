package rolecheck

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
	"github.com/rakunlabs/turna/pkg/server/model"
)

var (
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

type RoleCheck struct {
	PathMap     []PathMap `cfg:"path_map"`
	AllowOthers bool      `cfg:"allow_others"`

	Redirect Redirect `cfg:"redirect"`
}

type Redirect struct {
	Enable bool   `cfg:"enable"`
	URL    string `cfg:"url"`
}

type PathMap struct {
	RegexPath string `cfg:"regex_path"`
	Map       []Map  `cfg:"map"`

	regexPath *regexp.Regexp `cfg:"-"`
}

type Map struct {
	AllMethods    bool     `cfg:"all_methods"`
	ReadMethods   bool     `cfg:"read_methods"`
	WriteMethods  bool     `cfg:"write_methods"`
	Methods       []string `cfg:"methods"`
	Roles         []string `cfg:"roles"`
	RolesDisabled bool     `cfg:"roles_disabled"`

	methods map[string]struct{} `cfg:"-"`
}

func (m *RoleCheck) Middleware() (func(http.Handler) http.Handler, error) {
	for i := range m.PathMap {
		regexPath, err := regexp.Compile(m.PathMap[i].RegexPath)
		if err != nil {
			return nil, fmt.Errorf("invalid regex path: %s", m.PathMap[i].RegexPath)
		}

		m.PathMap[i].regexPath = regexPath

		for j := range m.PathMap[i].Map {
			methods := make(map[string]struct{}, len(m.PathMap[i].Map[j].Methods))
			for _, method := range m.PathMap[i].Map[j].Methods {
				methods[method] = struct{}{}
			}
			m.PathMap[i].Map[j].methods = methods
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			method := r.Method

			// get user roles from context
			claimValue, ok := tcontext.Get(r, "claims").(*claims.Custom)
			if !ok {
				httputil.JSON(w, http.StatusUnauthorized, model.MetaData{Message: "claims not found"})

				return
			}

			roles := claimValue.RoleSet

			for _, pathMap := range m.PathMap {
				if pathMap.regexPath.MatchString(path) {
					for _, m := range pathMap.Map {
						if m.AllMethods {
							if m.RolesDisabled {
								next.ServeHTTP(w, r)

								return
							}

							for _, role := range m.Roles {
								if _, ok := roles[role]; ok {
									next.ServeHTTP(w, r)

									return
								}
							}
						}

						if m.ReadMethods {
							if _, ok := ReadMethods[method]; ok {
								if m.RolesDisabled {
									next.ServeHTTP(w, r)

									return
								}

								for _, role := range m.Roles {
									if _, ok := roles[role]; ok {
										next.ServeHTTP(w, r)

										return
									}
								}
							}
						}

						if m.WriteMethods {
							if _, ok := WriteMethods[method]; ok {
								if m.RolesDisabled {
									next.ServeHTTP(w, r)

									return
								}

								for _, role := range m.Roles {
									if _, ok := roles[role]; ok {
										next.ServeHTTP(w, r)

										return
									}
								}
							}
						}

						if _, ok := m.methods[method]; ok {
							if m.RolesDisabled {
								next.ServeHTTP(w, r)

								return
							}

							for _, role := range m.Roles {
								if _, ok := roles[role]; ok {
									next.ServeHTTP(w, r)

									return
								}
							}
						}
					}

					if m.Redirect.Enable {
						httputil.Redirect(w, http.StatusTemporaryRedirect, m.Redirect.URL)

						return
					}

					httputil.JSON(w, http.StatusForbidden, model.MetaData{Message: "path not authorized"})

					return
				}
			}

			if m.AllowOthers {
				next.ServeHTTP(w, r)

				return
			}

			if m.Redirect.Enable {
				httputil.Redirect(w, http.StatusTemporaryRedirect, m.Redirect.URL)

				return
			}

			httputil.JSON(w, http.StatusForbidden, model.MetaData{Message: "path not authorized"})

			return
		})
	}, nil
}
