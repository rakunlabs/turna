package roledata

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/claims"
)

func TestRoleData_Middleware(t *testing.T) {
	type fields struct {
		Map     []Data
		Default interface{}
	}
	tests := []struct {
		name    string
		fields  fields
		want    string
		wantErr bool
	}{
		{
			name: "one",
			fields: fields{
				Map: []Data{
					{
						Roles: []string{"admin"},
						Data:  "admin data",
					},
				},
			},
			want: `["admin data"]`,
		},
		{
			name: "empty",
			fields: fields{
				Map: []Data{
					{
						Roles: []string{"not_exist"},
						Data:  "not_exist data",
					},
				},
			},
			want: "[]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &RoleData{
				Map:     tt.fields.Map,
				Default: tt.fields.Default,
			}

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			c.Set("claims", &claims.Custom{
				RoleSet: map[string]struct{}{
					"admin": {},
				},
			})

			middleware, _ := m.Middleware()
			middlewareInner := middleware(nil)
			err := middlewareInner(c)
			if (err != nil) != tt.wantErr {
				t.Errorf("RoleData.Middleware() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if strings.TrimSpace(rec.Body.String()) != tt.want {
				t.Errorf("RoleData.Middleware() = %q, want %q", rec.Body.String(), tt.want)
			}
		})
	}
}
