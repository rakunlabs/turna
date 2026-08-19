package auth

import (
	"testing"
	"time"
)

func TestValidateResource(t *testing.T) {
	valid := []string{
		"https://mcp.example.com/mcp",
		"https://example.com",
		"http://localhost:8080/api",
		"urn:example:resource",
	}
	for _, v := range valid {
		if err := validateResource(v); err != nil {
			t.Errorf("validateResource(%q) = %v, want nil", v, err)
		}
	}

	invalid := []string{
		"",
		"/relative/path",
		"https://example.com/mcp#frag",
		"://broken",
	}
	for _, v := range invalid {
		if err := validateResource(v); err == nil {
			t.Errorf("validateResource(%q) = nil, want error", v)
		}
	}
}

func TestResourcesAllowed(t *testing.T) {
	// empty allow list allows anything
	if !resourcesAllowed([]string{"https://a.example.com"}, nil) {
		t.Error("empty allow list should allow any resource")
	}

	allowed := []string{"https://mcp.example.com"}

	if !resourcesAllowed([]string{"https://mcp.example.com/mcp"}, allowed) {
		t.Error("prefix match should be allowed")
	}

	if resourcesAllowed([]string{"https://other.example.com"}, allowed) {
		t.Error("non-matching resource should be rejected")
	}

	if resourcesAllowed([]string{"https://mcp.example.com/mcp", "https://other.example.com"}, allowed) {
		t.Error("all requested resources must match")
	}

	// no requested resources is always fine
	if !resourcesAllowed(nil, allowed) {
		t.Error("no requested resources should be allowed")
	}
}

func TestRedirectURIAllowedForClient(t *testing.T) {
	// exact RedirectURIs take precedence
	client := AccessClient{
		RedirectURIs:  []string{"https://app.example.com/callback"},
		WhitelistURLs: []string{"https://other.example.com"},
	}

	if !client.redirectURIAllowedForClient("https://app.example.com/callback") {
		t.Error("exact redirect uri should be allowed")
	}

	if client.redirectURIAllowedForClient("https://app.example.com/callback2") {
		t.Error("non-exact redirect uri should be rejected when RedirectURIs is set")
	}

	if client.redirectURIAllowedForClient("https://other.example.com/x") {
		t.Error("whitelist must not apply when RedirectURIs is set")
	}

	// prefix whitelist fallback
	client = AccessClient{WhitelistURLs: []string{"https://app.example.com/"}}

	if !client.redirectURIAllowedForClient("https://app.example.com/callback") {
		t.Error("whitelist prefix should be allowed")
	}

	if client.redirectURIAllowedForClient("https://evil.example.com/") {
		t.Error("non-whitelisted redirect should be rejected")
	}

	// empty everything allows all (backwards compatible), except empty target
	client = AccessClient{}
	if !client.redirectURIAllowedForClient("https://anything.example.com") {
		t.Error("no restrictions should allow any redirect")
	}
	if client.redirectURIAllowedForClient("") {
		t.Error("empty redirect uri must be rejected")
	}
}

func TestTokenAudience(t *testing.T) {
	if aud := tokenAudience(nil); aud != "turna-auth" {
		t.Errorf("tokenAudience(nil) = %v, want turna-auth", aud)
	}

	aud, ok := tokenAudience([]string{"https://mcp.example.com"}).([]string)
	if !ok || len(aud) != 2 || aud[0] != "turna-auth" || aud[1] != "https://mcp.example.com" {
		t.Errorf("tokenAudience with resource = %v", aud)
	}
}

func TestAudienceResources(t *testing.T) {
	if res := audienceResources("turna-auth"); res != nil {
		t.Errorf("string aud should have no resources, got %v", res)
	}

	res := audienceResources([]any{"turna-auth", "https://mcp.example.com"})
	if len(res) != 1 || res[0] != "https://mcp.example.com" {
		t.Errorf("audienceResources = %v", res)
	}
}

func TestValidateRegistrationRedirectURI(t *testing.T) {
	valid := []string{
		"https://app.example.com/callback",
		"http://localhost:33418/callback",
		"http://127.0.0.1:8976/oauth/callback",
		"myapp://oauth/callback",
	}
	for _, v := range valid {
		if err := validateRegistrationRedirectURI(v); err != nil {
			t.Errorf("validateRegistrationRedirectURI(%q) = %v, want nil", v, err)
		}
	}

	invalid := []string{
		"",
		"/relative",
		"https://app.example.com/cb#frag",
	}
	for _, v := range invalid {
		if err := validateRegistrationRedirectURI(v); err == nil {
			t.Errorf("validateRegistrationRedirectURI(%q) = nil, want error", v)
		}
	}
}

func TestDynamicClientExpired(t *testing.T) {
	now := time.Now()

	static := AccessClient{CreatedAt: now.Add(-2 * time.Hour).Unix()}
	if dynamicClientExpired(static, time.Hour, now) {
		t.Error("static clients never expire")
	}

	dynamic := AccessClient{Dynamic: true, CreatedAt: now.Add(-2 * time.Hour).Unix()}
	if !dynamicClientExpired(dynamic, time.Hour, now) {
		t.Error("dynamic client past lifetime should expire")
	}

	if dynamicClientExpired(dynamic, 0, now) {
		t.Error("zero lifetime keeps dynamic clients forever")
	}

	fresh := AccessClient{Dynamic: true, CreatedAt: now.Add(-time.Minute).Unix()}
	if dynamicClientExpired(fresh, time.Hour, now) {
		t.Error("fresh dynamic client should not expire")
	}
}
