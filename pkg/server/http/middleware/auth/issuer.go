package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// Keyfunc returns the public key for access tokens signed by this middleware.
// It implements session.InfIssuer so session providers can validate tokens
// in-process with `auth_middleware: <name>`.
func (m *Auth) Keyfunc(token *jwt.Token) (any, error) {
	signer, err := m.jwtRuntime(context.Background())
	if err != nil {
		return nil, err
	}

	kid, _ := token.Header["kid"].(string)
	if kid != signer.KID {
		return nil, session.ErrKIDNotFound
	}

	return signer.Public, nil
}

// IssueToken runs the OAuth2 token endpoint in-process and returns the raw
// JSON body with its status code. It implements session.InfIssuer.
func (m *Auth) IssueToken(ctx context.Context, form url.Values) ([]byte, int, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, m.PrefixPath+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")

	rec := &responseRecorder{header: http.Header{}, code: http.StatusOK}
	m.APIToken(rec, r)

	return rec.body.Bytes(), rec.code, nil
}

// APIKeyData validates a raw static api key against the database and returns
// claim-shaped identity JSON for the key principal. It implements
// session.InfAPIKey; the session middleware calls it on every request that
// carries the api key header, so revocation is immediate.
func (m *Auth) APIKeyData(ctx context.Context, key string) ([]byte, error) {
	claims, err := m.apiKeyClaimsForKey(ctx, key)
	if err != nil {
		return nil, session.ErrTokenInvalid
	}

	return json.Marshal(claims)
}

// PasskeyToken runs the passkey login endpoint in-process. It implements
// session.InfPasskey so the login middleware can proxy WebAuthn ceremonies.
// The original request carries host/scheme used to derive the relying party.
func (m *Auth) PasskeyToken(ctx context.Context, orig *http.Request, body []byte) ([]byte, int, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, m.PrefixPath+"/oauth2/passkey", bytes.NewReader(body))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")

	if orig != nil {
		r.Host = orig.Host
		r.TLS = orig.TLS
		if v := orig.Header.Get("X-Forwarded-Proto"); v != "" {
			r.Header.Set("X-Forwarded-Proto", v)
		}
		if v := orig.Header.Get("X-Forwarded-Host"); v != "" {
			r.Header.Set("X-Forwarded-Host", v)
		}
	}

	rec := &responseRecorder{header: http.Header{}, code: http.StatusOK}
	m.APIPasskeyToken(rec, r)

	return rec.body.Bytes(), rec.code, nil
}

// responseRecorder is a minimal in-process http.ResponseWriter.
type responseRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (r *responseRecorder) Header() http.Header { return r.header }

func (r *responseRecorder) WriteHeader(code int) { r.code = code }

func (r *responseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
