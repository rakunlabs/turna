package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	oauth2auth "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/token"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/rakunlabs/turna/pkg/utils/jwks"

	"github.com/golang-jwt/jwt/v5"
)

// codeManager caches the oauth2 code client built from the "oauth2" setting namespace.
type codeManager struct {
	m       sync.Mutex
	cfg     OAuth2Settings
	baseURL string
	runtime *oauth2auth.Code
}

// codeRuntime returns the oauth2 code client built from the stored "oauth2"
// settings, rebuilding it when those settings change.
func (m *Auth) codeRuntime() (*oauth2auth.Code, error) {
	sn := m.cache.Snapshot()
	baseURL := m.effectiveOAuthBaseURL()

	m.code.m.Lock()
	defer m.code.m.Unlock()

	if m.code.runtime != nil && m.code.cfg == sn.OAuth2 && m.code.baseURL == baseURL {
		return m.code.runtime, nil
	}

	code := &oauth2auth.Code{
		BaseURL:            baseURL,
		Schema:             sn.OAuth2.Schema,
		Path:               m.PrefixPath + "/oauth2/code",
		InsecureSkipVerify: sn.OAuth2.InsecureSkipVerify,
	}

	if err := code.Init(); err != nil {
		return nil, err
	}

	m.code.runtime = code
	m.code.cfg = sn.OAuth2
	m.code.baseURL = baseURL

	return code, nil
}

const jwtSettingNamespace = "jwt"

type jwtSetting struct {
	KID        string `json:"kid"`
	PrivateKey string `json:"private_key"`
	// Previous holds the public keys of rotated signing keys. They keep
	// verifying (and stay in the JWKS) until RetireAt so tokens signed
	// before a rotation remain valid.
	Previous []jwtPreviousKey `json:"previous,omitempty"`
}

type jwtPreviousKey struct {
	KID       string `json:"kid"`
	PublicKey string `json:"public_key"`
	// RetireAt is the unix time after which the key is dropped.
	RetireAt int64 `json:"retire_at"`
}

func (s jwtSetting) equal(o jwtSetting) bool {
	return s.KID == o.KID && s.PrivateKey == o.PrivateKey && slices.Equal(s.Previous, o.Previous)
}

// jwtManager caches the JWT signer built from the encrypted "jwt" setting
// namespace; the signer is rebuilt when the stored setting changes.
type jwtManager struct {
	m        sync.Mutex
	cfg      jwtSetting
	jwt      *token.JWT
	kid      string
	public   *rsa.PublicKey
	previous []verifyKey
}

type verifyKey struct {
	KID      string
	Public   *rsa.PublicKey
	RetireAt time.Time
}

// jwtSigner bundles the active signing material and the still valid public
// keys of rotated signing keys.
type jwtSigner struct {
	JWT      *token.JWT
	KID      string
	Public   *rsa.PublicKey
	Previous []verifyKey
}

// VerifyKeys returns the active key first, then every rotated key that is not
// retired yet.
func (s *jwtSigner) VerifyKeys(now time.Time) []verifyKey {
	keys := []verifyKey{{KID: s.KID, Public: s.Public}}
	for _, k := range s.Previous {
		if now.Before(k.RetireAt) {
			keys = append(keys, k)
		}
	}

	return keys
}

// PublicKey returns the verification key for kid.
func (s *jwtSigner) PublicKey(kid string) (*rsa.PublicKey, bool) {
	for _, k := range s.VerifyKeys(time.Now()) {
		if k.KID == kid {
			return k.Public, true
		}
	}

	return nil, false
}

// Parse validates a token signed by the active or a not yet retired key.
func (s *jwtSigner) Parse(raw string, claims jwt.Claims, opts ...jwt.ParserOption) (*jwt.Token, error) {
	opts = append(opts, jwt.WithValidMethods([]string{s.JWT.Alg()}))
	parsed, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, ok := s.PublicKey(kid)
		if !ok {
			return nil, fmt.Errorf("unknown kid %q", kid)
		}

		return key, nil
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("token validate: %w", err)
	}

	return parsed, nil
}

func newRSAKeyPEM() (*rsa.PrivateKey, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", fmt.Errorf("generate rsa key: %w", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, "", fmt.Errorf("marshal rsa key: %w", err)
	}

	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})), nil
}

// generateJWTSetting creates and persists a fresh RSA signing key.
func (m *Auth) generateJWTSetting(ctx context.Context, updatedBy string) (jwtSetting, error) {
	return m.saveNewJWTKey(ctx, updatedBy, nil)
}

// rotateJWTSetting replaces the signing key. With grace > 0 the current key
// keeps verifying for that long; grace == 0 drops it (and every previous key)
// immediately, for a compromised key.
func (m *Auth) rotateJWTSetting(ctx context.Context, updatedBy string, grace time.Duration) (jwtSetting, error) {
	previous, err := retainedJWTKeys(m.cache.Snapshot().JWTKey, grace, time.Now())
	if err != nil {
		return jwtSetting{}, err
	}

	return m.saveNewJWTKey(ctx, updatedBy, previous)
}

// retainedJWTKeys returns the verification keys to keep after replacing the
// current signing key: unexpired previous keys plus the current key retired
// after grace. grace <= 0 keeps nothing.
func retainedJWTKeys(current jwtSetting, grace time.Duration, now time.Time) ([]jwtPreviousKey, error) {
	if grace <= 0 {
		return nil, nil
	}

	var previous []jwtPreviousKey
	for _, k := range current.Previous {
		if k.RetireAt > now.Unix() {
			previous = append(previous, k)
		}
	}

	if current.PrivateKey == "" {
		return previous, nil
	}

	privateKey, err := token.ParseRSAPrivateKey(current.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse current jwt key: %w", err)
	}

	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal current public key: %w", err)
	}

	kid := current.KID
	if kid == "" {
		kid = "turna-auth"
	}

	return append(previous, jwtPreviousKey{
		KID:       kid,
		PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
		RetireAt:  now.Add(grace).Unix(),
	}), nil
}

func (m *Auth) saveNewJWTKey(ctx context.Context, updatedBy string, previous []jwtPreviousKey) (jwtSetting, error) {
	_, privatePEM, err := newRSAKeyPEM()
	if err != nil {
		return jwtSetting{}, err
	}

	setting := jwtSetting{
		KID:        "turna-auth-" + ulid.Make().String(),
		PrivateKey: privatePEM,
		Previous:   previous,
	}

	settingRaw, err := json.Marshal(setting)
	if err != nil {
		return jwtSetting{}, err
	}

	if _, err := m.store.PutSetting(ctx, jwtSettingNamespace, settingRaw, updatedBy); err != nil {
		return jwtSetting{}, fmt.Errorf("save jwt setting: %w", err)
	}

	return setting, nil
}

func validateJWTSetting(setting jwtSetting) error {
	if setting.PrivateKey == "" {
		return errors.New("private_key is required")
	}

	if _, err := token.ParseRSAPrivateKey(setting.PrivateKey); err != nil {
		return fmt.Errorf("invalid private key: %w", err)
	}

	if setting.KID == "" {
		return errors.New("kid is required")
	}

	for _, k := range setting.Previous {
		if k.KID == "" || k.KID == setting.KID {
			return fmt.Errorf("previous key needs a kid different from the active kid")
		}

		if _, err := token.ParseRSAPublicKey(k.PublicKey); err != nil {
			return fmt.Errorf("invalid previous public key %q: %w", k.KID, err)
		}
	}

	return nil
}

func parsePreviousKeys(previous []jwtPreviousKey) []verifyKey {
	keys := make([]verifyKey, 0, len(previous))
	for _, k := range previous {
		public, err := token.ParseRSAPublicKey(k.PublicKey)
		if err != nil {
			slog.Warn("auth: skip invalid previous jwt key", "kid", k.KID, "error", err.Error())

			continue
		}

		keys = append(keys, verifyKey{KID: k.KID, Public: public, RetireAt: time.Unix(k.RetireAt, 0)})
	}

	return keys
}

// jwtRuntime returns the active signer, rebuilding it when the stored "jwt"
// setting changed and generating a fresh key when the setting is missing.
func (m *Auth) jwtRuntime(ctx context.Context) (*jwtSigner, error) {
	setting := m.cache.Snapshot().JWTKey

	m.jwtM.m.Lock()
	defer m.jwtM.m.Unlock()

	if m.jwtM.jwt != nil && m.jwtM.cfg.equal(setting) {
		return &jwtSigner{JWT: m.jwtM.jwt, KID: m.jwtM.kid, Public: m.jwtM.public, Previous: m.jwtM.previous}, nil
	}

	if setting.PrivateKey == "" {
		// first start or deleted setting: generate and persist a new key; the
		// create-only write lets the first instance win a concurrent start
		if _, err := m.generateJWTSetting(createOnlyContext(ctx), "system"); err != nil && !errors.Is(err, errPrecondition) {
			return nil, err
		}

		// reload so concurrent readers and other instances converge
		if err := m.cache.Reload(ctx); err != nil {
			return nil, fmt.Errorf("reload after jwt generate: %w", err)
		}
		setting = m.cache.Snapshot().JWTKey

		// the row exists but carries no key; replace it
		if setting.PrivateKey == "" {
			generated, err := m.generateJWTSetting(ctx, "system")
			if err != nil {
				return nil, err
			}
			setting = generated
		}
	}

	privateKey, err := token.ParseRSAPrivateKey(setting.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse jwt private key: %w", err)
	}

	kid := setting.KID
	if kid == "" {
		kid = "turna-auth"
	}

	jwtToken, err := token.NewJWT(
		token.WithKID(kid),
		token.WithMethod(jwt.SigningMethodRS256),
		token.WithRSAPrivateKey(privateKey),
	)
	if err != nil {
		return nil, fmt.Errorf("init jwt: %w", err)
	}

	m.jwtM.cfg = setting
	m.jwtM.jwt = jwtToken
	m.jwtM.kid = kid
	m.jwtM.public = &privateKey.PublicKey
	m.jwtM.previous = parsePreviousKeys(setting.Previous)

	return &jwtSigner{JWT: jwtToken, KID: kid, Public: m.jwtM.public, Previous: m.jwtM.previous}, nil
}

// ensureJWT warms up the signer at boot, generating the key on first start.
func (m *Auth) ensureJWT(ctx context.Context) error {
	_, err := m.jwtRuntime(ctx)

	return err
}

func clientSecretMatches(expected, provided string) bool {
	if expected == "" || provided == "" {
		return false
	}

	if subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1 {
		return true
	}

	// RFC 6749 §2.3.1 requires credentials in the Basic authorization header
	// to be form-urlencoded, so a secret holding "+", "/" or "=" arrives
	// percent-encoded. The verbatim value is compared first, keeping clients
	// that skip the encoding working.
	decoded, err := url.QueryUnescape(provided)
	if err != nil || decoded == provided {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(expected), []byte(decoded)) == 1
}

// GetAccessClient resolves and authenticates a confidential OAuth client.
func (m *Auth) GetAccessClient(clientID, clientSecret string) (*AccessClient, error) {
	client, ok := m.lookupClient(clientID)
	if !ok {
		return nil, fmt.Errorf("client %s not found", clientID)
	}

	if client.Public || !clientSecretMatches(client.ClientSecret, clientSecret) {
		return nil, fmt.Errorf("client secret mismatch")
	}

	return client, nil
}

// claimsFromProviderToken extracts the identity claims from an upstream
// provider token response, preferring the userinfo endpoint when configured.
func (m *Auth) claimsFromProviderToken(r *http.Request, tokenValue []byte, provider *session.Oauth2) (map[string]any, error) {
	accessToken, err := token.ParseAccessToken(tokenValue)
	if err != nil {
		return nil, err
	}

	if provider.UserInfoURL != "" {
		code, err := m.codeRuntime()
		if err != nil {
			return nil, err
		}

		userInfo, _, err := code.UserInfo(r.Context(), accessToken, provider)
		if err != nil {
			return nil, err
		}

		return userInfo, nil
	}

	claims := jwt.MapClaims{}

	// with cert_url the token signature is verified, without it the claims
	// are only as trusted as the back channel to the provider
	if provider.CertURL != "" {
		set, err := m.providerJWKS(provider.CertURL)
		if err != nil {
			return nil, fmt.Errorf("provider jwks: %w", err)
		}

		if _, err := jwt.ParseWithClaims(accessToken, &claims, set.Keyfunc); err != nil {
			return nil, fmt.Errorf("provider token: %w", err)
		}

		return claims, nil
	}

	slog.Warn("auth: provider identity read from an unverified access token, set userinfo_url or cert_url")

	if _, _, err := token.ParseAccessTokenJWT(accessToken, &claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// providerJWKS returns the cached key set of an upstream provider, refreshed
// in background until the middleware stops.
func (m *Auth) providerJWKS(certURL string) (*jwks.JWKS, error) {
	if v, ok := m.providerKeys.Load(certURL); ok {
		return v.(*jwks.JWKS), nil
	}

	ctx := m.runCtx
	if ctx == nil {
		ctx = context.Background()
	}

	set, err := jwks.Get(certURL, jwks.Options{
		Ctx:             ctx,
		Client:          &http.Client{Timeout: 30 * time.Second},
		RefreshInterval: 5 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	if v, loaded := m.providerKeys.LoadOrStore(certURL, set); loaded {
		set.EndBackground()

		return v.(*jwks.JWKS), nil
	}

	return set, nil
}

// aliasFromClaims picks the user alias from provider claims using the
// given key order.
func aliasFromClaims(claims map[string]any, keys ...string) (string, error) {
	for _, k := range keys {
		if alias, _ := claims[k].(string); alias != "" {
			return alias, nil
		}
	}

	return "", errors.New("alias not found in provider claims")
}

// GetOrCreateUser returns the user, attempting an LDAP sync when missing.
func (m *Auth) GetOrCreateUser(ctx context.Context, req data.GetUserRequest) (*data.UserExtended, error) {
	user, err := m.cache.GetUser(req)
	if err == nil {
		return user, nil
	}

	// attempt LDAP sync for the alias
	if req.Alias != "" && m.ldapEnabled() {
		if err := m.LdapSync(ctx, false, req.Alias); err != nil {
			return nil, err
		}

		return m.cache.GetUser(req)
	}

	return nil, err
}
