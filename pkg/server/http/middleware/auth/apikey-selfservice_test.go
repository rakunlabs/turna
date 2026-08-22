package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// testAuthAPIKeyGate builds an Auth whose snapshot has an admin permission the
// test user does not carry, so requireAdmin fails for regular X-Users.
func testAuthAPIKeyGate(selfService, disabled bool) *Auth {
	c := testCache()

	snap := c.Snapshot()
	noBreakGlass := false
	snap.Admin = AdminSettings{Permission: "auth-admin", AllowMissingXUser: &noBreakGlass}
	snap.APIKey = APIKeySettings{SelfService: selfService, Disabled: disabled}

	return &Auth{cache: c}
}

func TestAPIKeySelfOrAdmin(t *testing.T) {
	tests := []struct {
		name        string
		selfService bool
		disabled    bool
		xUser       string
		wantNext    bool
		wantCode    int
	}{
		{
			name:        "self service on lets a regular user through",
			selfService: true,
			xUser:       "my-user",
			wantNext:    true,
		},
		{
			name:     "self service off keeps the plane admin only",
			xUser:    "my-user",
			wantNext: false,
			wantCode: http.StatusForbidden,
		},
		{
			name:        "disabled api keys ignore self service",
			selfService: true,
			disabled:    true,
			xUser:       "my-user",
			wantNext:    false,
			wantCode:    http.StatusForbidden,
		},
		{
			name:        "self service still requires an X-User",
			selfService: true,
			xUser:       "",
			wantNext:    false,
			wantCode:    http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testAuthAPIKeyGate(tt.selfService, tt.disabled)

			nextCalled := false
			handler := m.apiKeySelfOrAdmin(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			r := httptest.NewRequest(http.MethodGet, "/v1/api-keys", nil)
			if tt.xUser != "" {
				r.Header.Set("X-User", tt.xUser)
			}

			w := httptest.NewRecorder()
			handler(w, r)

			if nextCalled != tt.wantNext {
				t.Fatalf("next called = %v, want %v (status %d)", nextCalled, tt.wantNext, w.Code)
			}

			if !tt.wantNext && w.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantCode)
			}
		})
	}
}

func TestCapabilitiesAPIKeySelfService(t *testing.T) {
	tests := []struct {
		name        string
		selfService bool
		disabled    bool
		xUser       string
		want        bool
	}{
		{name: "on with user", selfService: true, xUser: "my-user", want: true},
		{name: "off with user", selfService: false, xUser: "my-user", want: false},
		{name: "on but api keys disabled", selfService: true, disabled: true, xUser: "my-user", want: false},
		{name: "on without user", selfService: true, xUser: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testAuthAPIKeyGate(tt.selfService, tt.disabled)

			r := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
			if tt.xUser != "" {
				r.Header.Set("X-User", tt.xUser)
			}

			if got := m.capabilitiesForRequest(r).APIKeySelfService; got != tt.want {
				t.Fatalf("APIKeySelfService = %v, want %v", got, tt.want)
			}
		})
	}
}
