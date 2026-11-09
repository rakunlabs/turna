package scope

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

func scopeClaims(scope string) *claims.Custom {
	c := &claims.Custom{}
	if err := c.UnmarshalJSON([]byte(`{"scope":"` + scope + `"}`)); err != nil {
		panic(err)
	}

	return c
}

func TestScope(t *testing.T) {
	cases := map[string]struct {
		scope   Scope
		method  string
		vars    map[string]any
		noTurna bool
		want    int
	}{
		"matching scope":       {scope: Scope{Scopes: []string{"write", "read"}}, vars: map[string]any{KeyClaims: scopeClaims("openid read")}, want: http.StatusOK},
		"missing scope":        {scope: Scope{Scopes: []string{"admin"}}, vars: map[string]any{KeyClaims: scopeClaims("openid read")}, want: http.StatusForbidden},
		"no claims":            {scope: Scope{Scopes: []string{"read"}}, want: http.StatusUnauthorized},
		"no scopes configured": {scope: Scope{}, vars: map[string]any{KeyClaims: scopeClaims("")}, want: http.StatusOK},
		"method not covered":   {scope: Scope{Scopes: []string{"admin"}, Methods: []string{"post"}}, method: http.MethodGet, want: http.StatusOK},
		"method covered":       {scope: Scope{Scopes: []string{"admin"}, Methods: []string{"post"}}, method: http.MethodPost, vars: map[string]any{KeyClaims: scopeClaims("read")}, want: http.StatusForbidden},
		"disable flag":         {scope: Scope{Scopes: []string{"admin"}}, vars: map[string]any{DisableScopeCheckKey: true}, want: http.StatusOK},
		"auth noop flag":       {scope: Scope{Scopes: []string{"admin"}}, vars: map[string]any{KeyAuthNoop: true}, want: http.StatusOK},
		"noop config":          {scope: Scope{Scopes: []string{"admin"}, Noop: true}, want: http.StatusOK},
		"no turna context":     {scope: Scope{Scopes: []string{"read"}}, noTurna: true, want: http.StatusInternalServerError},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}

			r := httptest.NewRequest(method, "/", nil)
			if !tc.noTurna {
				_, r = tcontext.New(nil, r)
				for k, v := range tc.vars {
					tcontext.Set(r, k, v)
				}
			}

			called := false
			h := tc.scope.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if called != (tc.want == http.StatusOK) {
				t.Fatalf("next called = %v", called)
			}
		})
	}
}
