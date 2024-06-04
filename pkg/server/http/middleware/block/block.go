package block

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
)

type Block struct {
	Methods   []string `cfg:"methods"`
	RegexPath string   `cfg:"regex_path"`
}

func (m *Block) Middleware() (echo.MiddlewareFunc, error) {
	methodsSet := make(map[string]struct{}, len(m.Methods))
	for _, m := range m.Methods {
		methodsSet[strings.ToUpper(m)] = struct{}{}
	}

	var rgxPath *regexp.Regexp
	if m.RegexPath != "" {
		rgx, err := regexp.Compile(m.RegexPath)
		if err != nil {
			return nil, err
		}

		rgxPath = rgx
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if _, ok := methodsSet[c.Request().Method]; ok {
				return c.String(http.StatusForbidden, "Method not allowed")
			}

			if rgxPath != nil {
				if rgxPath.MatchString(c.Request().URL.Path) {
					return c.String(http.StatusForbidden, "Path not allowed")
				}
			}

			return next(c)
		}
	}, nil
}
