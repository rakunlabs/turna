package roledata

import (
	"net/http"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/worldline-go/turna/pkg/server/http/tcontext"
	"github.com/worldline-go/turna/pkg/server/model"
)

type RoleData struct {
	Map     []Data      `cfg:"map"`
	Default interface{} `cfg:"default"`
}

type Data struct {
	Roles []string    `cfg:"roles"`
	Data  interface{} `cfg:"data"`
}

func (m *RoleData) Middleware() (func(http.Handler) http.Handler, error) {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// get user roles from context
			claimValue, ok := tcontext.Get(r, "claims").(*claims.Custom)
			if !ok {
				httputil.JSON(w, http.StatusUnauthorized, model.MetaData{Message: "claims not found"})

				return
			}

			roles := claimValue.RoleSet

			values := []interface{}{}
			for _, data := range m.Map {
				for _, role := range data.Roles {
					if _, ok := roles[role]; ok {
						values = append(values, data.Data)

						break
					}
				}
			}

			if m.Default != nil {
				switch v := m.Default.(type) {
				case []interface{}:
					values = append(values, v...)
				default:
					values = append(values, v)
				}
			}

			httputil.JSON(w, http.StatusOK, values)
		})
	}, nil
}
