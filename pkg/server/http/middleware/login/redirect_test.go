package login

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
)

// authCodeReturn runs the response_type=code branch and returns the code that
// was stored, decoded.
func authCodeReturn(t *testing.T, target string) (*httptest.ResponseRecorder, store.Code) {
	t.Helper()

	storeCache, err := (&store.Store{}).Init(t.Context())
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	t.Cleanup(func() { _ = storeCache.Close() })

	m := &Login{store: storeCache}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)

	m.AuthCodeReturn(recorder, request, &claims.Custom{
		Map: map[string]any{"preferred_username": "user@example.com"},
	})

	if recorder.Code != http.StatusTemporaryRedirect {
		return recorder, store.Code{}
	}

	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}

	codeID := location.Query().Get("code")
	if codeID == "" {
		t.Fatal("redirect has no code")
	}

	raw, ok, err := storeCache.Code.Get(t.Context(), "code_"+codeID)
	if err != nil || !ok {
		t.Fatalf("code not stored: ok=%v err=%v", ok, err)
	}

	codeValue, err := store.Decode[store.Code](raw)
	if err != nil {
		t.Fatalf("decode code: %v", err)
	}

	return recorder, codeValue
}

// A code minted for a client must carry the client and redirect bindings,
// otherwise the auth middleware token endpoint rejects it with
// "code was issued to another client" (RFC 6749 §4.1.3).
func TestAuthCodeReturnBindsClientAndRedirect(t *testing.T) {
	redirectURI := "https://app.example.com/auth/code/turna"

	_, codeValue := authCodeReturn(t, "https://login.example.com/auth?response_type=code&client_id=my-client"+
		"&redirect_uri="+url.QueryEscape(redirectURI)+"&scope=openid+profile&state=xyz&nonce=n-1")

	if codeValue.ClientID != "my-client" {
		t.Errorf("ClientID = %q, want %q", codeValue.ClientID, "my-client")
	}
	if codeValue.RedirectURI != redirectURI {
		t.Errorf("RedirectURI = %q, want %q", codeValue.RedirectURI, redirectURI)
	}
	if codeValue.Nonce != "n-1" {
		t.Errorf("Nonce = %q", codeValue.Nonce)
	}
	if codeValue.Alias != "user@example.com" {
		t.Errorf("Alias = %q", codeValue.Alias)
	}
	if len(codeValue.Scope) != 2 || codeValue.Scope[0] != "openid" || codeValue.Scope[1] != "profile" {
		t.Errorf("Scope = %#v", codeValue.Scope)
	}
}

// An empty scope must not become a single empty entry.
func TestAuthCodeReturnEmptyScope(t *testing.T) {
	_, codeValue := authCodeReturn(t, "https://login.example.com/auth?response_type=code&client_id=my-client"+
		"&redirect_uri="+url.QueryEscape("https://app.example.com/cb"))

	if len(codeValue.Scope) != 0 {
		t.Errorf("Scope = %#v, want empty", codeValue.Scope)
	}
}

// A PKCE challenge must reach the code instead of being dropped silently.
func TestAuthCodeReturnCarriesPKCE(t *testing.T) {
	_, codeValue := authCodeReturn(t, "https://login.example.com/auth?response_type=code&client_id=my-client"+
		"&redirect_uri="+url.QueryEscape("https://app.example.com/cb")+
		"&code_challenge=abc&code_challenge_method=S256")

	if codeValue.CodeChallenge != "abc" || codeValue.CodeChallengeMethod != "S256" {
		t.Errorf("challenge = %q/%q", codeValue.CodeChallenge, codeValue.CodeChallengeMethod)
	}
}

func TestAuthCodeReturnRejectsBadPKCEMethod(t *testing.T) {
	recorder, _ := authCodeReturn(t, "https://login.example.com/auth?response_type=code&client_id=my-client"+
		"&redirect_uri="+url.QueryEscape("https://app.example.com/cb")+
		"&code_challenge=abc&code_challenge_method=md5")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestPKCEParams(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		challenge string
		method    string
		wantErr   bool
	}{
		{name: "empty", query: ""},
		{name: "default method", query: "code_challenge=abc", challenge: "abc", method: "plain"},
		{name: "s256", query: "code_challenge=abc&code_challenge_method=S256", challenge: "abc", method: "S256"},
		{name: "method without challenge", query: "code_challenge_method=S256", wantErr: true},
		{name: "unsupported method", query: "code_challenge=abc&code_challenge_method=md5", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, err := url.ParseQuery(test.query)
			if err != nil {
				t.Fatalf("parse query: %v", err)
			}

			challenge, method, err := pkceParams(query)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}

			if err != nil {
				t.Fatalf("pkceParams: %v", err)
			}
			if challenge != test.challenge || method != test.method {
				t.Fatalf("got %q/%q, want %q/%q", challenge, method, test.challenge, test.method)
			}
		})
	}
}
