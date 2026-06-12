package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	oauth2auth "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/token"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"

	"github.com/golang-jwt/jwt/v5"
)

// codeManager caches the oauth2 code client built from the "oauth2" setting namespace.
type codeManager struct {
	m       sync.Mutex
	cfg     OAuth2Settings
	runtime *oauth2auth.Code
}

// codeRuntime returns the oauth2 code client built from the stored "oauth2"
// settings, rebuilding it when those settings change.
func (m *Auth) codeRuntime() (*oauth2auth.Code, error) {
	sn := m.cache.Snapshot()

	m.code.m.Lock()
	defer m.code.m.Unlock()

	if m.code.runtime != nil && m.code.cfg == sn.OAuth2 {
		return m.code.runtime, nil
	}

	code := &oauth2auth.Code{
		BaseURL:            sn.OAuth2.BaseURL,
		Schema:             sn.OAuth2.Schema,
		Path:               m.PrefixPath + "/oauth2/code",
		InsecureSkipVerify: sn.OAuth2.InsecureSkipVerify,
	}

	if err := code.Init(); err != nil {
		return nil, err
	}

	m.code.runtime = code
	m.code.cfg = sn.OAuth2

	return code, nil
}

const jwtSettingNamespace = "jwt"

type jwtSetting struct {
	KID        string `json:"kid"`
	PrivateKey string `json:"private_key"`
}

// jwtManager caches the JWT signer built from the encrypted "jwt" setting
// namespace; the signer is rebuilt when the stored setting changes.
type jwtManager struct {
	m      sync.Mutex
	cfg    jwtSetting
	jwt    *token.JWT
	kid    string
	public *rsa.PublicKey
}

// jwtSigner bundles the active signing material.
type jwtSigner struct {
	JWT    *token.JWT
	KID    string
	Public *rsa.PublicKey
}

// generateJWTSetting creates and persists a fresh RSA signing key.
func (m *Auth) generateJWTSetting(ctx context.Context, updatedBy string) (jwtSetting, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return jwtSetting{}, fmt.Errorf("generate rsa key: %w", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return jwtSetting{}, fmt.Errorf("marshal rsa key: %w", err)
	}

	privatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateDER,
	})

	setting := jwtSetting{
		KID:        "turna-auth-" + ulid.Make().String(),
		PrivateKey: string(privatePEM),
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

	return nil
}

// jwtRuntime returns the active signer, rebuilding it when the stored "jwt"
// setting changed and generating a fresh key when the setting is missing.
func (m *Auth) jwtRuntime(ctx context.Context) (*jwtSigner, error) {
	setting := m.cache.Snapshot().JWTKey

	m.jwtM.m.Lock()
	defer m.jwtM.m.Unlock()

	if m.jwtM.jwt != nil && m.jwtM.cfg == setting {
		return &jwtSigner{JWT: m.jwtM.jwt, KID: m.jwtM.kid, Public: m.jwtM.public}, nil
	}

	if setting.PrivateKey == "" {
		// first start or deleted setting: generate and persist a new key
		generated, err := m.generateJWTSetting(ctx, "system")
		if err != nil {
			return nil, err
		}

		setting = generated

		// reload so concurrent readers and other instances converge
		if err := m.cache.Reload(ctx); err != nil {
			return nil, fmt.Errorf("reload after jwt generate: %w", err)
		}
		setting = m.cache.Snapshot().JWTKey
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

	return &jwtSigner{JWT: jwtToken, KID: kid, Public: m.jwtM.public}, nil
}

// ensureJWT warms up the signer at boot, generating the key on first start.
func (m *Auth) ensureJWT(ctx context.Context) error {
	_, err := m.jwtRuntime(ctx)

	return err
}

// GetAccessClient resolves the OAuth client; configured clients first, IAM service accounts as fallback.
func (m *Auth) GetAccessClient(clientID, clientSecret string) (*AccessClient, error) {
	sn := m.cache.Snapshot()

	client, ok := sn.OAuthClients[clientID]
	if !ok {
		// fallback to service account
		user, err := m.cache.GetUser(data.GetUserRequest{
			Alias:          clientID,
			ServiceAccount: &data.True,
		})
		if err != nil {
			return nil, fmt.Errorf("client %s not found", clientID)
		}

		secret, _ := user.Details["secret"].(string)
		scope, _ := user.Details["scope"].(string)
		whitelistURLs, _ := user.Details["whitelist_urls"].(string)

		client = AccessClient{
			ClientSecret:  secret,
			Scope:         splitFields(scope),
			WhitelistURLs: splitFields(whitelistURLs),
		}
	}

	if client.ClientSecret != clientSecret {
		return nil, fmt.Errorf("client secret mismatch")
	}

	return &client, nil
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
	if _, _, err := token.ParseAccessTokenJWT(accessToken, &claims); err != nil {
		return nil, err
	}

	return claims, nil
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
