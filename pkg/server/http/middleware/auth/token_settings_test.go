package auth

import (
	"testing"
	"time"
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
