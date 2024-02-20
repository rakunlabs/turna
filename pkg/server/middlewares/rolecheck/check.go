package rolecheck

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/claims"
	"github.com/worldline-go/turna/pkg/server/model"
)

type RoleCheck struct {
	PathMap     []PathMap `cfg:"path_map"`
	AllowOthers bool      `cfg:"allow_others"`
}

type PathMap struct {
	RegexPath string `cfg:"regex_path"`
	Map       []Map  `cfg:"map"`

	regexPath *regexp.Regexp `cfg:"-"`
}

type Map struct {
	Methods []string `cfg:"methods"`
	Roles   []string `cfg:"roles"`

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
				return c.JSON(http.StatusUnauthorized, model.MetaData{Error: "claims not found"})
			}

			roles := claimValue.RoleSet

			for _, pathMap := range m.PathMap {
				if pathMap.regexPath.MatchString(path) {
					for _, m := range pathMap.Map {
						if _, ok := m.methods[method]; ok {
							for _, role := range m.Roles {
								if _, ok := roles[role]; ok {
									return next(c)
								}
							}
						}
					}

					return c.JSON(http.StatusForbidden, model.MetaData{Error: "role not authorized"})
				}
			}

			if m.AllowOthers {
				return next(c)
			}

			return c.JSON(http.StatusForbidden, model.MetaData{Error: "path not authorized"})
		}
	}, nil
}
