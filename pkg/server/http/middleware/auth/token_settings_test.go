package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func TestTokenSettingsSessionLifetimes(t *testing.T) {
	defaults := TokenSettings{}
	if got := defaults.GetRefreshLifetime(); got != 24*time.Hour {
		t.Fatalf("refresh lifetime = %s", got)
	}
	if got := defaults.GetRefreshAbsoluteLifetime(); got != 30*24*time.Hour {
		t.Fatalf("absolute refresh lifetime = %s", got)
	}

	if err := validateTokenSettings(TokenSettings{
		TokenLifetime:           "15m",
		RefreshLifetime:         "24h",
		RefreshAbsoluteLifetime: "720h",
	}); err != nil {
		t.Fatalf("valid token settings: %v", err)
	}

	if err := validateTokenSettings(TokenSettings{
		RefreshLifetime:         "48h",
		RefreshAbsoluteLifetime: "24h",
	}); err == nil {
		t.Fatal("absolute lifetime shorter than idle lifetime was accepted")
	}

	if err := validateTokenSettings(TokenSettings{RefreshAbsoluteLifetime: "never"}); err == nil {
		t.Fatal("invalid duration was accepted")
	}
}

func TestWriteTokenRejectsDisabledPrincipal(t *testing.T) {
	m := &Auth{}

	for name, user := range map[string]*data.UserExtended{
		"missing principal": nil,
		"disabled user": {User: &data.User{
			ID:       "disabled-user",
			Disabled: true,
		}},
		"disabled service account": {User: &data.User{
			ID:             "disabled-service",
			Disabled:       true,
			ServiceAccount: true,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
			w := httptest.NewRecorder()

			m.writeTokenWithOptions(w, r, user, "client", nil, nil, tokenIssueOptions{})

			if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"error":"invalid_grant"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
