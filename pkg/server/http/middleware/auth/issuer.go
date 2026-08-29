package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// compile-time checks for the in-process issuer planes the session
// middleware can consume.
var (
	_ session.InfIssuer            = (*Auth)(nil)
	_ session.InfPublicPaths       = (*Auth)(nil)
	_ session.InfAccessChecker     = (*Auth)(nil)
	_ session.InfPasskey           = (*Auth)(nil)
	_ session.InfPasskeyEnrollment = (*Auth)(nil)
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
func (m *Auth) IssueToken(orig *http.Request, form url.Values) ([]byte, int, error) {
	r, err := http.NewRequestWithContext(orig.Context(), http.MethodPost, m.PrefixPath+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	r.Host = orig.Host
	r.TLS = orig.TLS
	for _, name := range []string{"X-Forwarded-Proto", "X-Forwarded-Host"} {
		if value := orig.Header.Get(name); value != "" {
			r.Header.Set(name, value)
		}
	}

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

func (m *Auth) PasskeyEnrollmentStatus(ctx context.Context, userID, method string) (session.PasskeyEnrollmentStatus, error) {
	cfg := m.cache.Snapshot().Passkey
	if cfg.Disabled || !cfg.Enrollment.Enabled || !cfg.Enrollment.AllowsMethod(method) {
		return session.PasskeyEnrollmentStatus{}, nil
	}

	user, err := m.cache.GetUser(data.GetUserRequest{ID: userID})
	if err != nil || user.Disabled {
		return session.PasskeyEnrollmentStatus{}, nil
	}

	credentials, err := m.store.ListPasskeyCredentials(ctx, userID)
	if err != nil {
		return session.PasskeyEnrollmentStatus{}, err
	}
	if len(credentials) > 0 && !cfg.Enrollment.PromptWhenRegistered {
		return session.PasskeyEnrollmentStatus{}, nil
	}

	policy, _ := json.Marshal(cfg.Enrollment)
	promptID := sha256.Sum256(append([]byte(m.PrefixPath+"\x00"+userID+"\x00"), policy...))

	return session.PasskeyEnrollmentStatus{
		Prompt:        true,
		PromptID:      fmt.Sprintf("%x", promptID[:]),
		SnoozeSeconds: int64(cfg.Enrollment.GetSnoozeDuration().Seconds()),
	}, nil
}

func (m *Auth) PasskeyEnrollmentRegister(ctx context.Context, orig *http.Request, userID, method string, body []byte) ([]byte, int, error) {
	status, err := m.PasskeyEnrollmentStatus(ctx, userID, method)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if !status.Prompt {
		return []byte(`{"message":"passkey enrollment is not available"}`), http.StatusForbidden, nil
	}

	var payload PasskeyRegisterRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, http.StatusBadRequest, err
	}
	payload.UserID = userID
	body, err = json.Marshal(payload)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	user, err := m.cache.GetUser(data.GetUserRequest{ID: userID})
	if err != nil {
		return nil, http.StatusUnauthorized, fmt.Errorf("enrollment user not found")
	}
	alias := ""
	for _, candidate := range user.Alias {
		if candidate != "" {
			alias = candidate
			break
		}
	}
	if alias == "" {
		return nil, http.StatusUnauthorized, fmt.Errorf("enrollment user alias not found")
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodPost, m.PrefixPath+"/v1/passkey/register", bytes.NewReader(body))
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-User", alias)
	if orig != nil {
		r.Host = orig.Host
		r.TLS = orig.TLS
		for _, name := range []string{"X-Forwarded-Proto", "X-Forwarded-Host"} {
			if value := orig.Header.Get(name); value != "" {
				r.Header.Set(name, value)
			}
		}
	}

	rec := &responseRecorder{header: http.Header{}, code: http.StatusOK}
	m.PasskeyRegisterAPI(rec, r)

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
