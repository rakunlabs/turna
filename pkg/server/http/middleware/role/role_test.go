package role

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

type roleCase struct {
	method string
	path   string
	// roles nil means no claims in context (anonymous request).
	roles []string

	isAllowed bool
}

func serve(t *testing.T, m *Role, method, path string, roles []string, setup func(*tcontext.Turna)) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	middleware, err := m.Middleware()
	if err != nil {
		t.Fatalf("Middleware() error = %v", err)
	}

	rec := httptest.NewRecorder()
	turna, req := tcontext.New(rec, httptest.NewRequest(method, path, nil))

	if roles != nil {
		roleSet := make(map[string]struct{}, len(roles))
		for _, role := range roles {
			roleSet[role] = struct{}{}
		}

		turna.Set(KeyClaims, &claims.Custom{RoleSet: roleSet})
	}

	if setup != nil {
		setup(turna)
	}

	var allowed bool
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed = true
	})).ServeHTTP(rec, req)

	return rec, allowed
}

func TestRole_Global(t *testing.T) {
	m := &Role{
		Roles:   []string{"admin"},
		Methods: []string{"get"},
	}

	cases := []roleCase{
		{method: http.MethodGet, path: "/", roles: []string{"admin"}, isAllowed: true},
		{method: http.MethodGet, path: "/", roles: []string{"user"}, isAllowed: false},
		{method: http.MethodGet, path: "/", isAllowed: false},
		{method: http.MethodPost, path: "/", roles: []string{"user"}, isAllowed: true},
		{method: http.MethodPost, path: "/", isAllowed: true},
	}

	for _, c := range cases {
		if _, allowed := serve(t, m, c.method, c.path, c.roles, nil); allowed != c.isAllowed {
			t.Errorf("%s %s roles=%v allowed = %v, want %v", c.method, c.path, c.roles, allowed, c.isAllowed)
		}
	}
}

func TestRole_PathMap(t *testing.T) {
	tests := []struct {
		name  string
		role  Role
		cases []roleCase
	}{
		{
			name: "empty",
			role: Role{},
			cases: []roleCase{
				{method: http.MethodGet, path: "/any", isAllowed: true},
			},
		},
		{
			name: "method groups",
			role: Role{
				PathMap: []PathMap{
					{RegexPath: "/test/.*", Map: []Map{{AllMethods: true, Roles: []string{"admin"}}}},
					{RegexPath: "/read/.*", Map: []Map{{ReadMethods: true, Roles: []string{"admin"}}}},
					{RegexPath: "/write/.*", Map: []Map{{WriteMethods: true, Roles: []string{"admin"}}}},
					{RegexPath: "/path/.*", Map: []Map{{WriteMethods: true, ReadMethods: true, Roles: []string{"admin"}}}},
					{RegexPath: "/no-role/.*", Map: []Map{{ReadMethods: true, RolesDisabled: true, Roles: []string{"admin"}}}},
				},
			},
			cases: []roleCase{
				{method: http.MethodGet, path: "/test/one", roles: []string{"admin"}, isAllowed: true},
				{method: http.MethodDelete, path: "/test/one", roles: []string{"admin"}, isAllowed: true},
				{method: http.MethodGet, path: "/test2/one", roles: []string{"admin"}, isAllowed: false},
				{method: http.MethodGet, path: "/read/one", roles: []string{"admin"}, isAllowed: true},
				{method: http.MethodDelete, path: "/read/one", roles: []string{"admin"}, isAllowed: false},
				{method: http.MethodGet, path: "/write/one", roles: []string{"admin"}, isAllowed: false},
				{method: http.MethodPost, path: "/write/one", roles: []string{"admin"}, isAllowed: true},
				{method: http.MethodGet, path: "/path/one", roles: []string{"admin"}, isAllowed: true},
				{method: http.MethodPost, path: "/path/one", roles: []string{"admin"}, isAllowed: true},
				{method: http.MethodGet, path: "/no-role/one", roles: []string{}, isAllowed: true},
				{method: http.MethodGet, path: "/no-role/one", isAllowed: true},
				{method: http.MethodDelete, path: "/no-role/one", roles: []string{}, isAllowed: false},
			},
		},
		{
			name: "diff roles",
			role: Role{
				PathMap: []PathMap{
					{
						RegexPath: "/test/(.*)",
						Map: []Map{
							{Methods: []string{http.MethodGet}, Roles: []string{"user_r", "user_rw"}},
							{Methods: []string{"delete"}, Roles: []string{"user_rw"}},
						},
					},
				},
			},
			cases: []roleCase{
				{method: http.MethodGet, path: "/test/one", roles: []string{"user_r"}, isAllowed: true},
				{method: http.MethodGet, path: "/test/one", roles: []string{"user_rw"}, isAllowed: true},
				{method: http.MethodDelete, path: "/test/one", roles: []string{"user_r"}, isAllowed: false},
				{method: http.MethodDelete, path: "/test/one", roles: []string{"user_r", "user_rw"}, isAllowed: true},
				{method: http.MethodDelete, path: "/other", roles: []string{}, isAllowed: false},
			},
		},
		{
			name: "allow others",
			role: Role{
				PathMap: []PathMap{
					{RegexPath: "/test/(.*)", Map: []Map{{Methods: []string{http.MethodGet}, Roles: []string{"user_r", "user_rw"}}}},
				},
				AllowOthers: true,
			},
			cases: []roleCase{
				{method: http.MethodGet, path: "/test/one", roles: []string{"user_r"}, isAllowed: true},
				{method: http.MethodGet, path: "/test/one", isAllowed: false},
				{method: http.MethodDelete, path: "/other", roles: []string{}, isAllowed: true},
				{method: http.MethodDelete, path: "/other", isAllowed: true},
			},
		},
		{
			name: "global and path map",
			role: Role{
				Roles: []string{"member"},
				PathMap: []PathMap{
					{RegexPath: "/admin/.*", Map: []Map{{AllMethods: true, Roles: []string{"admin"}}}},
				},
				AllowOthers: true,
			},
			cases: []roleCase{
				{method: http.MethodGet, path: "/home", roles: []string{"member"}, isAllowed: true},
				{method: http.MethodGet, path: "/home", roles: []string{"admin"}, isAllowed: false},
				{method: http.MethodGet, path: "/admin/x", roles: []string{"member"}, isAllowed: false},
				{method: http.MethodGet, path: "/admin/x", roles: []string{"member", "admin"}, isAllowed: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, c := range tt.cases {
				m := tt.role
				if _, allowed := serve(t, &m, c.method, c.path, c.roles, nil); allowed != c.isAllowed {
					t.Errorf("%s %s roles=%v allowed = %v, want %v", c.method, c.path, c.roles, allowed, c.isAllowed)
				}
			}
		})
	}
}

func TestRole_InvalidRegex(t *testing.T) {
	m := &Role{PathMap: []PathMap{{RegexPath: "("}}}
	if _, err := m.Middleware(); err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestRole_DenyResponses(t *testing.T) {
	m := &Role{Roles: []string{"admin"}}

	if rec, _ := serve(t, m, http.MethodGet, "/", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	if rec, _ := serve(t, m, http.MethodGet, "/", []string{"user"}, nil); rec.Code != http.StatusForbidden {
		t.Errorf("wrong role status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	m.Redirect = Redirect{Enable: true, URL: "/login"}
	rec, _ := serve(t, m, http.MethodGet, "/", []string{"user"}, nil)
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/login" {
		t.Errorf("redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRole_SkipFlags(t *testing.T) {
	m := &Role{Roles: []string{"admin"}}

	for _, key := range []string{DisableRoleCheckKey, KeyAuthNoop, session.CtxPublicAccessKey} {
		_, allowed := serve(t, m, http.MethodGet, "/", nil, func(turna *tcontext.Turna) {
			turna.Set(key, true)
		})
		if !allowed {
			t.Errorf("%s: expected request to pass", key)
		}
	}

	m.Noop = true
	if _, allowed := serve(t, m, http.MethodGet, "/", nil, nil); !allowed {
		t.Error("noop: expected request to pass")
	}
}

func TestRole_Data(t *testing.T) {
	tests := []struct {
		name  string
		role  Role
		roles []string
		want  string
		code  int
	}{
		{
			name:  "one",
			role:  Role{Data: &Data{Map: []DataMap{{Roles: []string{"admin"}, Data: "admin data"}}}},
			roles: []string{"admin"},
			want:  `["admin data"]`,
			code:  http.StatusOK,
		},
		{
			name:  "empty",
			role:  Role{Data: &Data{Map: []DataMap{{Roles: []string{"not_exist"}, Data: "x"}}}},
			roles: []string{"admin"},
			want:  `[]`,
			code:  http.StatusOK,
		},
		{
			name: "default",
			role: Role{Data: &Data{
				Map:     []DataMap{{Roles: []string{"admin"}, Data: 1}},
				Default: []any{2, 3},
			}},
			roles: []string{"admin"},
			want:  `[1,2,3]`,
			code:  http.StatusOK,
		},
		{
			name: "anonymous gets default",
			role: Role{Data: &Data{
				Map:     []DataMap{{Roles: []string{"admin"}, Data: 1}},
				Default: "public",
			}},
			want: `["public"]`,
			code: http.StatusOK,
		},
		{
			name:  "check before data",
			role:  Role{Roles: []string{"admin"}, Data: &Data{Default: "x"}},
			roles: []string{"user"},
			code:  http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, allowed := serve(t, &tt.role, http.MethodGet, "/", tt.roles, nil)
			if allowed {
				t.Error("data mode must not call next handler")
			}

			if rec.Code != tt.code {
				t.Errorf("status = %d, want %d", rec.Code, tt.code)
			}

			if tt.want != "" && strings.TrimSpace(rec.Body.String()) != tt.want {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.want)
			}
		})
	}
}
