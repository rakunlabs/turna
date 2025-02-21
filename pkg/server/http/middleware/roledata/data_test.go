package roledata

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/worldline-go/turna/pkg/server/http/tcontext"
)

func TestRoleData_Middleware(t *testing.T) {
	type fields struct {
		Map     []Data
		Default interface{}
	}
	tests := []struct {
		name   string
		fields fields
		want   string
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

			rec := httptest.NewRecorder()
			turna, req := tcontext.New(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			turna.Set("claims", &claims.Custom{
				RoleSet: map[string]struct{}{
					"admin": {},
				},
			})

			middleware, _ := m.Middleware()
			middlewareInner := middleware(nil)
			middlewareInner.ServeHTTP(rec, req)

			if strings.TrimSpace(rec.Body.String()) != tt.want {
				t.Errorf("RoleData.Middleware() = %q, want %q", rec.Body.String(), tt.want)
			}
		})
	}
}
