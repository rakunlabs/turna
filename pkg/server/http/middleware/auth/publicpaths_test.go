package auth

import (
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

func TestPublicPathPatterns(t *testing.T) {
	m := &Auth{PrefixPath: "/auth"}
	patterns := m.PublicPathPatterns()

	match := func(path string) bool {
		for _, pattern := range patterns {
			if ok, _ := doublestar.Match(pattern, path); ok {
				return true
			}
		}

		return false
	}

	public := []string{
		"/auth/oauth2/token",
		"/auth/oauth2/code/gitlab",
		"/auth/oauth2/auth/gitlab",
		"/auth/oauth2/authorize",
		"/auth/oauth2/consent",
		"/auth/oauth2/certs",
		"/auth/oauth2/userinfo",
		"/auth/oauth2/register",
		"/auth/oauth2/device_authorization",
		"/auth/oauth2/.well-known/oauth-authorization-server",
		"/auth/saml/idp/acs",
		"/.well-known/openid-configuration",
		"/.well-known/oauth-authorization-server",
	}
	for _, path := range public {
		if !match(path) {
			t.Errorf("public path %q must match a pattern", path)
		}
	}

	protected := []string{
		"/auth/v1/users",
		"/auth/v1/settings",
		"/auth/v1/api-keys",
		"/auth/ui/",
		"/app/page",
	}
	for _, path := range protected {
		if match(path) {
			t.Errorf("protected path %q must not match any pattern", path)
		}
	}
}

func TestPublicPathPatternsEmptyPrefix(t *testing.T) {
	m := &Auth{}

	match := func(path string) bool {
		for _, pattern := range m.PublicPathPatterns() {
			if ok, _ := doublestar.Match(pattern, path); ok {
				return true
			}
		}

		return false
	}

	if !match("/oauth2/token") {
		t.Error("/oauth2/token must match with an empty prefix")
	}
	if match("/v1/users") {
		t.Error("/v1/users must not match")
	}
}

func TestCredentialPassthroughPathPatterns(t *testing.T) {
	m := &Auth{PrefixPath: "/auth"}
	patterns := m.CredentialPassthroughPathPatterns()

	if len(patterns) != 1 || patterns[0] != "/auth/oauth2/api-key" {
		t.Fatalf("patterns = %v, want [/auth/oauth2/api-key]", patterns)
	}
}
