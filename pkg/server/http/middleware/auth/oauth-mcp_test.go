package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestValidateClientMetadataURL(t *testing.T) {
	valid := []string{"https://client.example/app.json", "https://client.example:8443/oauth/client"}
	for _, clientID := range valid {
		if _, err := validateClientMetadataURL(clientID); err != nil {
			t.Errorf("validateClientMetadataURL(%q) = %v", clientID, err)
		}
	}

	invalid := []string{
		"http://client.example/app.json",
		"https://client.example",
		"https://client.example/a/../app.json",
		"https://user@client.example/app.json",
		"https://client.example/app.json#fragment",
	}
	for _, clientID := range invalid {
		if _, err := validateClientMetadataURL(clientID); err == nil {
			t.Errorf("validateClientMetadataURL(%q) = nil, want error", clientID)
		}
	}
}

func TestDecodeClientMetadata(t *testing.T) {
	clientID := "https://client.example/app.json"
	client, err := decodeClientMetadata(clientID, []byte(`{
		"client_id":"https://client.example/app.json",
		"client_name":"Example MCP Client",
		"redirect_uris":["http://127.0.0.1:3000/callback"],
		"token_endpoint_auth_method":"none",
		"grant_types":["authorization_code","refresh_token"],
		"response_types":["code"]
	}`))
	if err != nil {
		t.Fatalf("decodeClientMetadata: %v", err)
	}
	if !client.Public || client.ClientName != "Example MCP Client" || len(client.RedirectURIs) != 1 {
		t.Fatalf("client = %+v", client)
	}

	bad := []string{
		`{"client_id":"https://other.example/app.json","client_name":"x","redirect_uris":["https://app.example/cb"]}`,
		`{"client_id":"https://client.example/app.json","redirect_uris":["https://app.example/cb"]}`,
		`{"client_id":"https://client.example/app.json","client_name":"x","redirect_uris":["https://app.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
	}
	for _, body := range bad {
		if _, err := decodeClientMetadata(clientID, []byte(body)); err == nil {
			t.Errorf("decodeClientMetadata(%s) = nil, want error", body)
		}
	}
}

func TestAuthorizeErrorRedirectIncludesIssuer(t *testing.T) {
	m := &Auth{PrefixPath: "/auth"}
	r := httptest.NewRequest(http.MethodGet, "https://auth.example/auth/oauth2/authorize", nil)
	w := httptest.NewRecorder()

	m.authorizeErrorRedirect(w, r, "https://client.example/callback", "state-1", "invalid_request", "bad request")
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if got := location.Query().Get("iss"); got != "https://auth.example/auth/oauth2" {
		t.Fatalf("iss = %q", got)
	}
	if location.Query().Get("state") != "state-1" || location.Query().Get("error") != "invalid_request" {
		t.Fatalf("redirect query = %v", location.Query())
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
