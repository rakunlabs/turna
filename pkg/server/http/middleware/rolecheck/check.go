package rolecheck

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/model"
	"github.com/worldline-go/auth/claims"
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

func (m *RoleCheck) Middleware() (echo.MiddlewareFunc, error) {
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

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			request := c.Request()
			path := request.URL.Path
			method := request.Method

			// get user roles from context
			claimValue, ok := c.Get("claims").(*claims.Custom)
			if !ok {
				return c.JSON(http.StatusUnauthorized, model.MetaData{Message: "claims not found"})
			}

			roles := claimValue.RoleSet

			for _, pathMap := range m.PathMap {
				if pathMap.regexPath.MatchString(path) {
					for _, m := range pathMap.Map {
						if m.AllMethods {
							if m.RolesDisabled {
								return next(c)
							}

							for _, role := range m.Roles {
								if _, ok := roles[role]; ok {
									return next(c)
								}
							}
						}

						if m.ReadMethods {
							if _, ok := ReadMethods[method]; ok {
								if m.RolesDisabled {
									return next(c)
								}

								for _, role := range m.Roles {
									if _, ok := roles[role]; ok {
										return next(c)
									}
								}
							}
						}

						if m.WriteMethods {
							if _, ok := WriteMethods[method]; ok {
								if m.RolesDisabled {
									return next(c)
								}

								for _, role := range m.Roles {
									if _, ok := roles[role]; ok {
										return next(c)
									}
								}
							}
						}

						if _, ok := m.methods[method]; ok {
							if m.RolesDisabled {
								return next(c)
							}

							for _, role := range m.Roles {
								if _, ok := roles[role]; ok {
									return next(c)
								}
							}
						}
					}

					if m.Redirect.Enable {
						return c.Redirect(http.StatusTemporaryRedirect, m.Redirect.URL)
					}

					return c.JSON(http.StatusForbidden, model.MetaData{Message: "role not authorized"})
				}
			}

			if m.AllowOthers {
				return next(c)
			}

			if m.Redirect.Enable {
				return c.Redirect(http.StatusTemporaryRedirect, m.Redirect.URL)
			}

			return c.JSON(http.StatusForbidden, model.MetaData{Message: "path not authorized"})
		}
	}, nil
}
