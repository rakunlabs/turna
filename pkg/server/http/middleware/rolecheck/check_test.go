package rolecheck

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

func TestRoleCheck_Middleware(t *testing.T) {
	type fields struct {
		PathMap     []PathMap
		AllowOthers bool
		Redirect    Redirect
	}
	type casex struct {
		method string
		path   string

		roles []string

		isAllowed bool
	}
	tests := []struct {
		name    string
		fields  fields
		Cases   []casex
		wantErr bool
	}{
		{
			name:   "empty",
			fields: fields{},
		},
		{
			name: "one",
			fields: fields{
				PathMap: []PathMap{
					{
						RegexPath: "/test/.*",
						Map: []Map{
							{
								AllMethods: true,
								Roles:      []string{"admin"},
							},
						},
					},
					{
						RegexPath: "/read/.*",
						Map: []Map{
							{
								ReadMethods: true,
								Roles:       []string{"admin"},
							},
						},
					},
					{
						RegexPath: "/write/.*",
						Map: []Map{
							{
								WriteMethods: true,
								Roles:        []string{"admin"},
							},
						},
					},
					{
						RegexPath: "/path/.*",
						Map: []Map{
							{
								WriteMethods: true,
								ReadMethods:  true,
								Roles:        []string{"admin"},
							},
						},
					},
					{
						RegexPath: "/no-role/.*",
						Map: []Map{
							{
								ReadMethods:   true,
								RolesDisabled: true,
								Roles:         []string{"admin"},
							},
						},
					},
				},
			},
			Cases: []casex{
				{
					method:    http.MethodGet,
					path:      "/test/one",
					roles:     []string{"admin"},
					isAllowed: true,
				},
				{
					method:    http.MethodDelete,
					path:      "/test/one",
					roles:     []string{"admin"},
					isAllowed: true,
				},
				{
					method:    http.MethodGet,
					path:      "/test2/one",
					roles:     []string{"admin"},
					isAllowed: false,
				},
				{
					method:    http.MethodGet,
					path:      "/read/one",
					roles:     []string{"admin"},
					isAllowed: true,
				},
				{
					method:    http.MethodDelete,
					path:      "/read/one",
					roles:     []string{"admin"},
					isAllowed: false,
				},
				{
					method:    http.MethodGet,
					path:      "/write/one",
					roles:     []string{"admin"},
					isAllowed: false,
				},
				{
					method:    http.MethodPost,
					path:      "/write/one",
					roles:     []string{"admin"},
					isAllowed: true,
				},
				{
					method:    http.MethodGet,
					path:      "/path/one",
					roles:     []string{"admin"},
					isAllowed: true,
				},
				{
					method:    http.MethodPost,
					path:      "/path/one",
					roles:     []string{"admin"},
					isAllowed: true,
				},
				{
					method:    http.MethodGet,
					path:      "/no-role/one",
					isAllowed: true,
				},
				{
					method:    http.MethodDelete,
					path:      "/no-role/one",
					isAllowed: false,
				},
			},
		},
		{
			name: "diff roles",
			fields: fields{
				PathMap: []PathMap{
					{
						RegexPath: "/test/(.*)",
						Map: []Map{
							{
								Methods: []string{http.MethodGet},
								Roles:   []string{"user_r", "user_rw"},
							},
							{
								Methods: []string{http.MethodDelete},
								Roles:   []string{"user_rw"},
							},
						},
					},
				},
			},
			Cases: []casex{
				{
					method:    http.MethodGet,
					path:      "/test/one",
					roles:     []string{"user_r"},
					isAllowed: true,
				},
				{
					method:    http.MethodGet,
					path:      "/test/one",
					roles:     []string{"user_rw"},
					isAllowed: true,
				},
				{
					method:    http.MethodDelete,
					path:      "/test/one",
					roles:     []string{"user_r"},
					isAllowed: false,
				},
				{
					method:    http.MethodDelete,
					path:      "/test/one",
					roles:     []string{"user_r", "user_rw"},
					isAllowed: true,
				},
				{
					method:    http.MethodDelete,
					path:      "/other",
					isAllowed: false,
				},
			},
		},
		{
			name: "allow other",
			fields: fields{
				PathMap: []PathMap{
					{
						RegexPath: "/test/(.*)",
						Map: []Map{
							{
								Methods: []string{http.MethodGet},
								Roles:   []string{"user_r", "user_rw"},
							},
						},
					},
				},
				AllowOthers: true,
			},
			Cases: []casex{
				{
					method:    http.MethodGet,
					path:      "/test/one",
					roles:     []string{"user_r"},
					isAllowed: true,
				},
				{
					method:    http.MethodDelete,
					path:      "/other",
					isAllowed: true,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &RoleCheck{
				PathMap:     tt.fields.PathMap,
				AllowOthers: tt.fields.AllowOthers,
				Redirect:    tt.fields.Redirect,
			}

			got, err := m.Middleware()
			if (err != nil) != tt.wantErr {
				t.Errorf("RoleCheck.Middleware() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for i := range tt.Cases {
				rec := httptest.NewRecorder()
				turna, req := tcontext.New(rec, httptest.NewRequest(tt.Cases[i].method, tt.Cases[i].path, nil))

				roles := make(map[string]struct{}, len(tt.Cases[i].roles))
				for _, role := range tt.Cases[i].roles {
					roles[role] = struct{}{}
				}

				turna.Set("claims", &claims.Custom{
					RoleSet: roles,
				})

				var allowed bool
				got(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					allowed = true
				})).ServeHTTP(rec, req)

				if allowed != tt.Cases[i].isAllowed {
					t.Errorf("RoleCheck.Middleware() = %v, want %v", allowed, tt.Cases[i].isAllowed)
				}
			}
		})
	}
}
