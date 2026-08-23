package session

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type fakeIssuer struct {
	kid      string
	key      any
	lastForm url.Values
	issueM   sync.Mutex
	issues   int
	delay    time.Duration
}

func (f *fakeIssuer) Keyfunc(token *jwt.Token) (any, error) {
	if kid, _ := token.Header["kid"].(string); kid != f.kid {
		return nil, ErrKIDNotFound
	}

	return f.key, nil
}

func (f *fakeIssuer) IssueToken(_ *http.Request, form url.Values) ([]byte, int, error) {
	f.issueM.Lock()
	f.lastForm = form
	f.issues++
	f.issueM.Unlock()

	time.Sleep(f.delay)

	return []byte(`{"grant_type":"` + form.Get("grant_type") + `"}`), 200, nil
}

func (f *fakeIssuer) issueCount() int {
	f.issueM.Lock()
	defer f.issueM.Unlock()

	return f.issues
}

func TestIssuerKeyFunc(t *testing.T) {
	IssuerRegistry.Set("test-auth", &fakeIssuer{kid: "kid-1", key: "public-key"})

	keyFunc := &issuerKeyFunc{providers: map[string]string{"turna": "test-auth"}}

	token := &jwt.Token{Header: map[string]any{"kid": "kid-1"}}
	key, err := keyFunc.Keyfunc(token)
	if err != nil {
		t.Fatalf("keyfunc: %v", err)
	}
	if key != "public-key" {
		t.Fatalf("key = %v", key)
	}
	if name, _ := token.Header["provider_name"].(string); name != "turna" {
		t.Fatalf("provider_name = %q", name)
	}

	// unknown kid falls through with ErrKIDNotFound
	token = &jwt.Token{Header: map[string]any{"kid": "other"}}
	if _, err := keyFunc.Keyfunc(token); !errors.Is(err, ErrKIDNotFound) {
		t.Fatalf("expected ErrKIDNotFound, got %v", err)
	}

	// unknown issuer name also falls through
	missing := &issuerKeyFunc{providers: map[string]string{"x": "missing"}}
	if _, err := missing.Keyfunc(&jwt.Token{Header: map[string]any{}}); !errors.Is(err, ErrKIDNotFound) {
		t.Fatalf("expected ErrKIDNotFound, got %v", err)
	}
}

func TestIssuerRefreshTokenData(t *testing.T) {
	issuer := &fakeIssuer{kid: "kid-1", key: "public-key"}
	IssuerRegistry.Set("refresh-auth", issuer)

	m := &Session{
		Provider: map[string]Provider{
			"turna": {
				AuthMiddleware: "refresh-auth",
				Oauth2:         &Oauth2{ClientID: "ui", ClientSecret: "secret"},
			},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	body, err := m.refreshTokenData(r, "turna", &TokenData{RefreshToken: "r1"})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if string(body) != `{"grant_type":"refresh_token"}` {
		t.Fatalf("body = %s", body)
	}
	if issuer.lastForm.Get("refresh_token") != "r1" || issuer.lastForm.Get("client_id") != "ui" ||
		issuer.lastForm.Get("client_secret") != "secret" {
		t.Fatalf("refresh form = %v", issuer.lastForm)
	}

	if _, err := m.refreshTokenData(r, "unknown", &TokenData{}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestIssuerRefreshTokenDataCollapsesConcurrentRefresh(t *testing.T) {
	issuer := &fakeIssuer{kid: "kid-1", key: "public-key", delay: 20 * time.Millisecond}
	IssuerRegistry.Set("concurrent-refresh-auth", issuer)

	m := &Session{
		Provider: map[string]Provider{
			"turna": {
				AuthMiddleware: "concurrent-refresh-auth",
				Oauth2:         &Oauth2{ClientID: "ui"},
			},
		},
	}
	r := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	token := &TokenData{RefreshToken: "single-use-refresh-token"}

	const requests = 8
	var wg sync.WaitGroup
	errC := make(chan error, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, err := m.refreshTokenData(r, "turna", token)
			errC <- err
		}()
	}

	wg.Wait()
	close(errC)
	for err := range errC {
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
	}

	if got := issuer.issueCount(); got != 1 {
		t.Fatalf("issuer calls = %d, want 1", got)
	}
}
