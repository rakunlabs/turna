package auth

import "testing"

func TestCodeStoreSettingsDefaultToDatabase(t *testing.T) {
	t.Parallel()

	if got := (CodeStoreSettings{}).normalized().Active; got != "database" {
		t.Fatalf("default code store = %q, want database", got)
	}
}

func TestValidateCodeStoreSettings(t *testing.T) {
	t.Parallel()

	for _, active := range []string{"", "database", "memory", "redis", " REDIS "} {
		if err := validateCodeStoreSettings(CodeStoreSettings{Active: active}); err != nil {
			t.Errorf("active %q rejected: %v", active, err)
		}
	}

	if err := validateCodeStoreSettings(CodeStoreSettings{Active: "unknown"}); err == nil {
		t.Fatal("unknown code store should be rejected")
	}
}
