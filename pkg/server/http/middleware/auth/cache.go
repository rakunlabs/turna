package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/worldline-go/types"
	"github.com/xhit/go-str2duration/v2"
)

var DefaultCachePollInterval = 5 * time.Second

// AccessClient is the decoded OAuth client config stored in auth_oauth_clients.
type AccessClient struct {
	ClientSecret  string   `json:"client_secret"`
	Scope         []string `json:"scope"`
	WhitelistURLs []string `json:"whitelist_urls"`
	// RolesClaim overrides the global token roles_claim dot path for tokens
	// issued to this client. Empty falls back to TokenSettings.GetRolesClaim().
	RolesClaim string `json:"roles_claim"`
}

// ProviderConfig is the decoded OAuth provider config stored in auth_oauth_providers.
type ProviderConfig struct {
	ClientID      string   `json:"client_id"`
	ClientSecret  string   `json:"client_secret"`
	Scopes        []string `json:"scopes"`
	CertURL       string   `json:"cert_url"`
	IntrospectURL string   `json:"introspect_url"`
	UserInfoURL   string   `json:"userinfo_url"`
	RevocationURL string   `json:"revocation_url"`
	AuthURL       string   `json:"auth_url"`
	TokenURL      string   `json:"token_url"`
	LogoutURL     string   `json:"logout_url"`

	// ClaimMapping maps provider claims to local users and roles.
	ClaimMapping ClaimMapping `json:"claim_mapping"`
}

func (p ProviderConfig) Session() *session.Oauth2 {
	return &session.Oauth2{
		ClientID:      p.ClientID,
		ClientSecret:  p.ClientSecret,
		Scopes:        p.Scopes,
		CertURL:       p.CertURL,
		IntrospectURL: p.IntrospectURL,
		UserInfoURL:   p.UserInfoURL,
		RevocationURL: p.RevocationURL,
		AuthURL:       p.AuthURL,
		TokenURL:      p.TokenURL,
		LogoutURL:     p.LogoutURL,
	}
}

// LDAPSettings is the decoded LDAP config stored in auth_ldap_configs.
type LDAPSettings struct {
	Addr string `json:"addr"`
	Bind struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"bind"`
	UserBaseDN string `json:"user_base_dn"`
	Groups     []struct {
		BaseDN     string   `json:"base_dn"`
		Filter     string   `json:"filter"`
		Attributes []string `json:"attributes"`
	} `json:"groups"`
	SyncDuration string `json:"sync_duration"`
	DisableSync  bool   `json:"disable_sync"`
}

// OAuth2Settings is the decoded "oauth2" setting namespace.
// It controls the code-flow redirect behavior for upstream providers.
type OAuth2Settings struct {
	// BaseURL for redirect URLs. Default is the request host.
	BaseURL string `json:"base_url"`
	// Schema for redirect URLs when base_url is empty. Default https.
	Schema string `json:"schema"`

	InsecureSkipVerify bool `json:"insecure_skip_verify"`
}

// CustomInfoSettings is the decoded "custom_info" setting namespace.
// Each named set maps an output claim name to a mugo/Go template rendered
// against the user's base claims and full profile. A template whose key is
// not already a claim ADDS a new claim; an existing key OVERWRITES it.
// The set is selected by the {custom} path segment of /oauth2/userinfo/{custom}.
type CustomInfoSettings struct {
	// Disabled turns off custom userinfo templating entirely.
	Disabled bool `json:"disabled"`
	// Sets maps a custom name (the {custom} path value) to its claim templates.
	Sets map[string]CustomInfoSet `json:"sets"`
}

// CustomInfoSet is a single named group of userinfo claim templates.
type CustomInfoSet struct {
	// Claims maps an output claim name to a mugo/Go template string. Templates
	// receive {"claims": <base claims>, "user": <full user record>} as data.
	Claims map[string]string `json:"claims"`
}

// CheckSettings is the decoded "check" setting namespace.
type CheckSettings struct {
	// DefaultHosts used when a permission resource has no hosts.
	DefaultHosts []string `json:"default_hosts"`
	// NoHostCheck disables host checking on permission resources.
	NoHostCheck bool `json:"no_host_check"`
}

func (c CheckSettings) Config() data.CheckConfig {
	return data.CheckConfig{
		DefaultHosts: c.DefaultHosts,
		NoHostCheck:  c.NoHostCheck,
	}
}

// CacheSettings is the decoded "cache" setting namespace.
type CacheSettings struct {
	// PollInterval for version polling between instances. Default 5s.
	PollInterval string `json:"poll_interval"`
	// CodeStore configures the temporary OAuth2 code/state cache. Default memory.
	CodeStore CodeStoreSettings `json:"code_store"`

	pollInterval time.Duration
}

func (c CacheSettings) GetPollInterval() time.Duration {
	if c.pollInterval > 0 {
		return c.pollInterval
	}

	return DefaultCachePollInterval
}

// PasswordSettings is the decoded "password" setting namespace.
// It controls which credential sources the password grant accepts.
// All defaults keep the implicit behavior: local users check bcrypt,
// non-local users bind against LDAP, unknown aliases are created from LDAP.
type PasswordSettings struct {
	// Disabled turns off the password grant entirely.
	Disabled bool `json:"disabled"`
	// LocalDisabled blocks password login for local users.
	LocalDisabled bool `json:"local_disabled"`
	// LdapDisabled blocks LDAP password checks for non-local users.
	LdapDisabled bool `json:"ldap_disabled"`
	// LdapRegisterDisabled stops creating unknown users from LDAP at login.
	LdapRegisterDisabled bool `json:"ldap_register_disabled"`
}

// PasskeySettings is the decoded "passkey" setting namespace.
// Empty values are derived from the request: rp_id from the host and
// origins from the forwarded scheme/host.
type PasskeySettings struct {
	// Disabled turns off passkey registration and login endpoints.
	Disabled bool `json:"disabled"`
	// RPID is the WebAuthn relying party ID (bare host, e.g. "example.com").
	RPID string `json:"rp_id"`
	// RPDisplayName is shown by the platform passkey UI. Default "Turna Auth".
	RPDisplayName string `json:"rp_display_name"`
	// Origins allowed in WebAuthn client data (e.g. "https://example.com").
	Origins []string `json:"origins"`
	// UserVerification is required, preferred (default) or discouraged.
	UserVerification string `json:"user_verification"`
}

// APIKeySettings is the decoded "api_key" setting namespace.
type APIKeySettings struct {
	// Disabled turns off api key creation and validation.
	Disabled bool `json:"disabled"`
	// MaxLifetime caps the expiry of newly created keys (duration string).
	// Empty means keys may live forever.
	MaxLifetime string `json:"max_lifetime"`

	maxLifetime time.Duration
}

func (a APIKeySettings) GetMaxLifetime() time.Duration {
	return a.maxLifetime
}

// DeviceSettings is the decoded "device" setting namespace (RFC 8628).
type DeviceSettings struct {
	// Disabled turns off the device authorization flow.
	Disabled bool `json:"disabled"`
	// CodeLifetime of device/user codes. Default 10m.
	CodeLifetime string `json:"code_lifetime"`
	// Interval minimum polling interval in seconds. Default 5.
	Interval int `json:"interval"`
	// VerificationURI shown to the user. Default <prefix>/ui/device.
	VerificationURI string `json:"verification_uri"`

	codeLifetime time.Duration
}

func (d DeviceSettings) GetCodeLifetime() time.Duration {
	if d.codeLifetime > 0 {
		return d.codeLifetime
	}

	return 10 * time.Minute
}

func (d DeviceSettings) GetInterval() int {
	if d.Interval > 0 {
		return d.Interval
	}

	return 5
}

// TokenExchangeSettings is the decoded "token_exchange" setting namespace (RFC 8693).
type TokenExchangeSettings struct {
	// Disabled turns off the token exchange grant.
	Disabled bool `json:"disabled"`
}

// TOTPSettings is the decoded "totp" setting namespace.
type TOTPSettings struct {
	// Disabled turns off totp registration and enforcement.
	Disabled bool `json:"disabled"`
	// Issuer shown in authenticator apps. Default "Turna Auth".
	Issuer string `json:"issuer"`
	// Skew allowed periods in each direction. Default 1 (+/-30s).
	Skew *int `json:"skew"`
}

func (t TOTPSettings) GetIssuer() string {
	if t.Issuer != "" {
		return t.Issuer
	}

	return "Turna Auth"
}

func (t TOTPSettings) GetSkew() int {
	if t.Skew != nil && *t.Skew >= 0 {
		return *t.Skew
	}

	return 1
}

// SMTPSettings configures the mail relay for email login.
type SMTPSettings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	// NoAuth skips SMTP AUTH even when username is set. Useful for trusted
	// relays where username is only used as the default From address.
	NoAuth bool `json:"no_auth"`
	// StartTLS upgrades a plain connection. Default port 587.
	StartTLS bool `json:"starttls"`
	// TLS uses an implicit TLS connection. Default port 465.
	TLS                bool `json:"tls"`
	InsecureSkipVerify bool `json:"insecure_skip_verify"`
}

func (s SMTPSettings) GetPort() int {
	if s.Port > 0 {
		return s.Port
	}

	if s.TLS {
		return 465
	}

	return 587
}

// EmailSettings is the decoded "email" setting namespace. It controls two
// independent passwordless flows: the one-time code mail and the magic-link
// mail. Each has its own enable flag and Go text/template subject + body.
type EmailSettings struct {
	// Disabled turns off the one-time code email login even when smtp is
	// configured. The magic link is controlled separately by MagicLink.
	Disabled bool `json:"disabled"`
	// MagicLink enables the magic-link login mail when a redirect_uri is
	// provided and allowed by the client whitelist. Default true.
	MagicLink *bool `json:"magic_link"`
	// From address; defaults to the smtp username.
	From string `json:"from"`
	// Subject of the one-time code mail (Go text/template).
	// Default "Your login code".
	Subject string `json:"subject"`
	// BodyTemplate is the Go text/template body of the one-time code mail.
	// Empty uses the built-in code template.
	BodyTemplate string `json:"body_template"`
	// MagicLinkSubject is the subject of the magic-link mail (Go text/template).
	// Empty uses the built-in default.
	MagicLinkSubject string `json:"magic_link_subject"`
	// MagicLinkBodyTemplate is the Go text/template body of the magic-link
	// mail. Empty uses the built-in magic-link template.
	MagicLinkBodyTemplate string `json:"magic_link_body_template"`
	// CodeLifetime of login codes. Default 15m.
	CodeLifetime string       `json:"code_lifetime"`
	SMTP         SMTPSettings `json:"smtp"`

	codeLifetime time.Duration
}

func (e EmailSettings) GetCodeLifetime() time.Duration {
	if e.codeLifetime > 0 {
		return e.codeLifetime
	}

	return 15 * time.Minute
}

func (e EmailSettings) GetMagicLink() bool {
	if e.MagicLink != nil {
		return *e.MagicLink
	}

	return true
}

// SignupSettings is the decoded "signup" setting namespace for
// self-registration and password reset over email. Everything is optional
// and managed from the UI; signup is off by default.
type SignupSettings struct {
	// Enabled allows self-registration through /oauth2/signup.
	Enabled bool `json:"enabled"`
	// EmailVerification requires confirming the address before the account
	// is created. Default true; without it signup creates active users.
	EmailVerification *bool `json:"email_verification"`
	// PasswordReset enables the forgot-password flow; independent of Enabled.
	PasswordReset bool `json:"password_reset"`
	// DefaultRoleIDs are granted to users created through signup.
	DefaultRoleIDs []string `json:"default_role_ids"`
	// PasswordMinLength is the minimum length for signup/reset/change
	// passwords. Default 8 when unset (0).
	PasswordMinLength int `json:"password_min_length"`
	// CodeLifetime of verification/reset codes. Default 1h.
	CodeLifetime string `json:"code_lifetime"`

	// Go text/template mail templates; empty uses built-in defaults.
	VerifySubject      string `json:"verify_subject"`
	VerifyBodyTemplate string `json:"verify_body_template"`
	ResetSubject       string `json:"reset_subject"`
	ResetBodyTemplate  string `json:"reset_body_template"`

	codeLifetime time.Duration
}

func (s SignupSettings) GetEmailVerification() bool {
	if s.EmailVerification != nil {
		return *s.EmailVerification
	}

	return true
}

// GetPasswordMinLength returns the configured minimum password length, or the
// built-in default (minPasswordLength) when unset.
func (s SignupSettings) GetPasswordMinLength() int {
	if s.PasswordMinLength > 0 {
		return s.PasswordMinLength
	}

	return minPasswordLength
}

func (s SignupSettings) GetCodeLifetime() time.Duration {
	if s.codeLifetime > 0 {
		return s.codeLifetime
	}

	return time.Hour
}

// MTLSSettings is the decoded "mtls" setting namespace (RFC 8705 style).
type MTLSSettings struct {
	// Enabled allows certificate based client authentication.
	Enabled bool `json:"enabled"`
	// CertHeader is a trusted header carrying the client certificate set
	// by a TLS-terminating proxy (e.g. "ssl-client-cert" from nginx with
	// $ssl_client_escaped_cert). Only set this behind a trusted proxy.
	CertHeader string `json:"cert_header"`
}

// TokenSettings is the decoded "token" setting namespace.
type TokenSettings struct {
	TokenLifetime   string `json:"token_lifetime"`
	RefreshLifetime string `json:"refresh_lifetime"`
	// RolesClaim is the dot path where the scope-derived roles are written
	// in the access token. Empty defaults to "roles" (flat top-level array).
	// Use "realm_access.roles" for Keycloak-style nesting, or any dot path
	// such as "resource_access.app.roles". A per-client override in
	// AccessClient.RolesClaim takes precedence when set.
	RolesClaim string `json:"roles_claim"`

	tokenLifetime   time.Duration
	refreshLifetime time.Duration
}

func (t TokenSettings) GetTokenLifetime() time.Duration {
	if t.tokenLifetime > 0 {
		return t.tokenLifetime
	}

	return 15 * time.Minute
}

func (t TokenSettings) GetRefreshLifetime() time.Duration {
	if t.refreshLifetime > 0 {
		return t.refreshLifetime
	}

	return 24 * time.Hour
}

// GetRolesClaim returns the configured dot path for the roles claim, or the
// flat "roles" default when unset.
func (t TokenSettings) GetRolesClaim() string {
	if t.RolesClaim != "" {
		return t.RolesClaim
	}

	return "roles"
}

// AdminSettings controls access to auth management APIs/UI. Empty permission
// keeps bootstrap compatibility: authenticated users, and break-glass requests
// without X-User, are treated as admin.
type AdminSettings struct {
	// Permission is matched against permission id or name on the X-User.
	Permission string `json:"permission"`
	// AdminPermission is accepted as a legacy/explicit alias for Permission.
	AdminPermission string `json:"admin_permission,omitempty"`
	// AllowMissingXUser allows break-glass admin access when the session chain
	// is removed and no X-User header is present. Default true.
	AllowMissingXUser *bool `json:"allow_missing_x_user"`
}

func (a AdminSettings) GetPermission() string {
	if a.Permission != "" {
		return a.Permission
	}

	return a.AdminPermission
}

func (a AdminSettings) GetAllowMissingXUser() bool {
	if a.AllowMissingXUser != nil {
		return *a.AllowMissingXUser
	}

	return true
}

// Snapshot is the immutable in-memory read model of the auth database.
type Snapshot struct {
	Version uint64

	Users   map[string]*data.User
	UserIDs []string
	Alias   map[string]string

	Roles       map[string]*data.Role
	RoleIDs     []string
	RoleNames   map[string]string
	Permissions map[string]*data.Permission
	PermIDs     []string
	PermNames   map[string]string

	LMaps     map[string]*data.LMap
	LMapNames []string

	OAuthClients   map[string]AccessClient
	OAuthProviders map[string]ProviderConfig
	LDAP           []LDAPSettings
	SAMLProviders  map[string]SAMLProviderConfig

	Token         TokenSettings
	Admin         AdminSettings
	OAuth2        OAuth2Settings
	Check         data.CheckConfig
	Cache         CacheSettings
	Passkey       PasskeySettings
	Password      PasswordSettings
	JWTKey        jwtSetting
	APIKey        APIKeySettings
	Device        DeviceSettings
	TokenExchange TokenExchangeSettings
	TOTP          TOTPSettings
	Email         EmailSettings
	Signup        SignupSettings
	MTLS          MTLSSettings
	SAMLKey       samlSetting
	CustomInfo    CustomInfoSettings
}

// Cache keeps the snapshot up to date with polling and explicit reloads.
type Cache struct {
	store *Store

	snap atomic.Pointer[Snapshot]
	m    sync.Mutex
}

func NewCache(store *Store) *Cache {
	c := &Cache{store: store}
	c.snap.Store(&Snapshot{})

	return c
}

func (c *Cache) Snapshot() *Snapshot {
	return c.snap.Load()
}

func (c *Cache) Reload(ctx context.Context) error {
	c.m.Lock()
	defer c.m.Unlock()

	version, err := c.store.Version(ctx)
	if err != nil {
		return fmt.Errorf("load version: %w", err)
	}

	users, err := c.store.LoadUsers(ctx)
	if err != nil {
		return fmt.Errorf("load users: %w", err)
	}

	roles, err := c.store.LoadRoles(ctx)
	if err != nil {
		return fmt.Errorf("load roles: %w", err)
	}

	permissions, err := c.store.LoadPermissions(ctx)
	if err != nil {
		return fmt.Errorf("load permissions: %w", err)
	}

	lmaps, err := c.store.LoadLMaps(ctx)
	if err != nil {
		return fmt.Errorf("load lmaps: %w", err)
	}

	clientsRaw, err := c.store.LoadConfigResources(ctx, oauthClientKind)
	if err != nil {
		return fmt.Errorf("load oauth clients: %w", err)
	}

	providersRaw, err := c.store.LoadConfigResources(ctx, oauthProviderKind)
	if err != nil {
		return fmt.Errorf("load oauth providers: %w", err)
	}

	ldapRaw, err := c.store.LoadConfigResources(ctx, ldapConfigKind)
	if err != nil {
		return fmt.Errorf("load ldap configs: %w", err)
	}

	tokenRaw, err := c.store.GetSettingValue(ctx, "token")
	if err != nil {
		return fmt.Errorf("load token settings: %w", err)
	}

	adminRaw, err := c.store.GetSettingValue(ctx, "admin")
	if err != nil {
		return fmt.Errorf("load admin settings: %w", err)
	}

	oauth2Raw, err := c.store.GetSettingValue(ctx, "oauth2")
	if err != nil {
		return fmt.Errorf("load oauth2 settings: %w", err)
	}

	checkRaw, err := c.store.GetSettingValue(ctx, "check")
	if err != nil {
		return fmt.Errorf("load check settings: %w", err)
	}

	cacheRaw, err := c.store.GetSettingValue(ctx, "cache")
	if err != nil {
		return fmt.Errorf("load cache settings: %w", err)
	}

	passkeyRaw, err := c.store.GetSettingValue(ctx, "passkey")
	if err != nil {
		return fmt.Errorf("load passkey settings: %w", err)
	}

	passwordRaw, err := c.store.GetSettingValue(ctx, "password")
	if err != nil {
		return fmt.Errorf("load password settings: %w", err)
	}

	jwtRaw, err := c.store.GetSettingValue(ctx, jwtSettingNamespace)
	if err != nil {
		return fmt.Errorf("load jwt settings: %w", err)
	}

	apiKeyRaw, err := c.store.GetSettingValue(ctx, "api_key")
	if err != nil {
		return fmt.Errorf("load api_key settings: %w", err)
	}

	deviceRaw, err := c.store.GetSettingValue(ctx, "device")
	if err != nil {
		return fmt.Errorf("load device settings: %w", err)
	}

	tokenExchangeRaw, err := c.store.GetSettingValue(ctx, "token_exchange")
	if err != nil {
		return fmt.Errorf("load token_exchange settings: %w", err)
	}

	totpRaw, err := c.store.GetSettingValue(ctx, "totp")
	if err != nil {
		return fmt.Errorf("load totp settings: %w", err)
	}

	emailRaw, err := c.store.GetSettingValue(ctx, "email")
	if err != nil {
		return fmt.Errorf("load email settings: %w", err)
	}

	signupRaw, err := c.store.GetSettingValue(ctx, "signup")
	if err != nil {
		return fmt.Errorf("load signup settings: %w", err)
	}

	mtlsRaw, err := c.store.GetSettingValue(ctx, "mtls")
	if err != nil {
		return fmt.Errorf("load mtls settings: %w", err)
	}

	samlKeyRaw, err := c.store.GetSettingValue(ctx, samlSettingNamespace)
	if err != nil {
		return fmt.Errorf("load saml settings: %w", err)
	}

	customInfoRaw, err := c.store.GetSettingValue(ctx, "custom_info")
	if err != nil {
		return fmt.Errorf("load custom_info settings: %w", err)
	}

	samlProvidersRaw, err := c.store.LoadConfigResources(ctx, samlProviderKind)
	if err != nil {
		return fmt.Errorf("load saml providers: %w", err)
	}

	snap := &Snapshot{
		Version:        version,
		Users:          make(map[string]*data.User, len(users)),
		UserIDs:        make([]string, 0, len(users)),
		Alias:          map[string]string{},
		Roles:          make(map[string]*data.Role, len(roles)),
		RoleIDs:        make([]string, 0, len(roles)),
		RoleNames:      map[string]string{},
		Permissions:    make(map[string]*data.Permission, len(permissions)),
		PermIDs:        make([]string, 0, len(permissions)),
		PermNames:      map[string]string{},
		LMaps:          make(map[string]*data.LMap, len(lmaps)),
		LMapNames:      make([]string, 0, len(lmaps)),
		OAuthClients:   map[string]AccessClient{},
		OAuthProviders: map[string]ProviderConfig{},
		SAMLProviders:  map[string]SAMLProviderConfig{},
	}

	for _, user := range users {
		snap.Users[user.ID] = user
		snap.UserIDs = append(snap.UserIDs, user.ID)

		for _, alias := range user.Alias {
			snap.Alias[alias] = user.ID
		}
	}
	sort.Strings(snap.UserIDs)

	for _, role := range roles {
		snap.Roles[role.ID] = role
		snap.RoleIDs = append(snap.RoleIDs, role.ID)
		snap.RoleNames[role.Name] = role.ID
	}
	sort.Strings(snap.RoleIDs)

	for _, permission := range permissions {
		snap.Permissions[permission.ID] = permission
		snap.PermIDs = append(snap.PermIDs, permission.ID)
		snap.PermNames[permission.Name] = permission.ID
	}
	sort.Strings(snap.PermIDs)

	for _, lmap := range lmaps {
		snap.LMaps[lmap.Name] = lmap
		snap.LMapNames = append(snap.LMapNames, lmap.Name)
	}
	sort.Strings(snap.LMapNames)

	for id, raw := range clientsRaw {
		var client AccessClient
		if err := json.Unmarshal(raw, &client); err != nil {
			slog.Warn("invalid oauth client config", slog.String("id", id), slog.String("error", err.Error()))
			continue
		}

		snap.OAuthClients[id] = client
	}

	for id, raw := range providersRaw {
		var provider ProviderConfig
		if err := json.Unmarshal(raw, &provider); err != nil {
			slog.Warn("invalid oauth provider config", slog.String("id", id), slog.String("error", err.Error()))
			continue
		}

		snap.OAuthProviders[id] = provider
	}

	ldapIDs := make([]string, 0, len(ldapRaw))
	for id := range ldapRaw {
		ldapIDs = append(ldapIDs, id)
	}
	sort.Strings(ldapIDs)

	for _, id := range ldapIDs {
		var ldapCfg LDAPSettings
		if err := json.Unmarshal(ldapRaw[id], &ldapCfg); err != nil {
			slog.Warn("invalid ldap config", slog.String("id", id), slog.String("error", err.Error()))
			continue
		}

		if ldapCfg.Addr != "" {
			snap.LDAP = append(snap.LDAP, ldapCfg)
		}
	}

	if tokenRaw != nil {
		_ = json.Unmarshal(tokenRaw, &snap.Token)
	}
	if snap.Token.TokenLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Token.TokenLifetime); err == nil {
			snap.Token.tokenLifetime = d
		}
	}
	if snap.Token.RefreshLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Token.RefreshLifetime); err == nil {
			snap.Token.refreshLifetime = d
		}
	}

	if adminRaw != nil {
		if err := json.Unmarshal(adminRaw, &snap.Admin); err != nil {
			slog.Warn("invalid admin settings", slog.String("error", err.Error()))
		}
	}

	if oauth2Raw != nil {
		if err := json.Unmarshal(oauth2Raw, &snap.OAuth2); err != nil {
			slog.Warn("invalid oauth2 settings", slog.String("error", err.Error()))
		}
	}

	if checkRaw != nil {
		var checkCfg CheckSettings
		if err := json.Unmarshal(checkRaw, &checkCfg); err != nil {
			slog.Warn("invalid check settings", slog.String("error", err.Error()))
		} else {
			snap.Check = checkCfg.Config()
		}
	}

	if cacheRaw != nil {
		if err := json.Unmarshal(cacheRaw, &snap.Cache); err != nil {
			slog.Warn("invalid cache settings", slog.String("error", err.Error()))
		}
	}
	if snap.Cache.PollInterval != "" {
		if d, err := str2duration.ParseDuration(snap.Cache.PollInterval); err == nil {
			snap.Cache.pollInterval = d
		}
	}

	if passkeyRaw != nil {
		if err := json.Unmarshal(passkeyRaw, &snap.Passkey); err != nil {
			slog.Warn("invalid passkey settings", slog.String("error", err.Error()))
		}
	}

	if passwordRaw != nil {
		if err := json.Unmarshal(passwordRaw, &snap.Password); err != nil {
			slog.Warn("invalid password settings", slog.String("error", err.Error()))
		}
	}

	if jwtRaw != nil {
		if err := json.Unmarshal(jwtRaw, &snap.JWTKey); err != nil {
			slog.Warn("invalid jwt settings", slog.String("error", err.Error()))
		}
	}

	if apiKeyRaw != nil {
		if err := json.Unmarshal(apiKeyRaw, &snap.APIKey); err != nil {
			slog.Warn("invalid api_key settings", slog.String("error", err.Error()))
		}
	}
	if snap.APIKey.MaxLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.APIKey.MaxLifetime); err == nil {
			snap.APIKey.maxLifetime = d
		}
	}

	if deviceRaw != nil {
		if err := json.Unmarshal(deviceRaw, &snap.Device); err != nil {
			slog.Warn("invalid device settings", slog.String("error", err.Error()))
		}
	}
	if snap.Device.CodeLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Device.CodeLifetime); err == nil {
			snap.Device.codeLifetime = d
		}
	}

	if tokenExchangeRaw != nil {
		if err := json.Unmarshal(tokenExchangeRaw, &snap.TokenExchange); err != nil {
			slog.Warn("invalid token_exchange settings", slog.String("error", err.Error()))
		}
	}

	if totpRaw != nil {
		if err := json.Unmarshal(totpRaw, &snap.TOTP); err != nil {
			slog.Warn("invalid totp settings", slog.String("error", err.Error()))
		}
	}

	if emailRaw != nil {
		if err := json.Unmarshal(emailRaw, &snap.Email); err != nil {
			slog.Warn("invalid email settings", slog.String("error", err.Error()))
		}
	}
	if snap.Email.CodeLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Email.CodeLifetime); err == nil {
			snap.Email.codeLifetime = d
		}
	}

	if signupRaw != nil {
		if err := json.Unmarshal(signupRaw, &snap.Signup); err != nil {
			slog.Warn("invalid signup settings", slog.String("error", err.Error()))
		}
	}
	if snap.Signup.CodeLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Signup.CodeLifetime); err == nil {
			snap.Signup.codeLifetime = d
		}
	}

	if mtlsRaw != nil {
		if err := json.Unmarshal(mtlsRaw, &snap.MTLS); err != nil {
			slog.Warn("invalid mtls settings", slog.String("error", err.Error()))
		}
	}

	if samlKeyRaw != nil {
		if err := json.Unmarshal(samlKeyRaw, &snap.SAMLKey); err != nil {
			slog.Warn("invalid saml settings", slog.String("error", err.Error()))
		}
	}

	if customInfoRaw != nil {
		if err := json.Unmarshal(customInfoRaw, &snap.CustomInfo); err != nil {
			slog.Warn("invalid custom_info settings", slog.String("error", err.Error()))
		}
	}

	for id, raw := range samlProvidersRaw {
		var provider SAMLProviderConfig
		if err := json.Unmarshal(raw, &provider); err != nil {
			slog.Warn("invalid saml provider config", slog.String("id", id), slog.String("error", err.Error()))
			continue
		}

		snap.SAMLProviders[id] = provider
	}

	c.snap.Store(snap)

	return nil
}

// Watch polls the auth version and reloads the snapshot when it changes.
// The poll interval comes from the "cache" setting namespace and is applied live.
func (c *Cache) Watch(ctx context.Context) {
	interval := c.Snapshot().Cache.GetPollInterval()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			version, err := c.store.Version(ctx)
			if err != nil {
				slog.Error("auth cache version check failed", slog.String("error", err.Error()))
				continue
			}

			if version != c.Snapshot().Version {
				if err := c.Reload(ctx); err != nil {
					slog.Error("auth cache reload failed", slog.String("error", err.Error()))
				}
			}

			if newInterval := c.Snapshot().Cache.GetPollInterval(); newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
			}
		}
	}
}

// ////////////////////////////////////////////////////////////////////
// role helpers

// virtualRoleIDs expands roles to include roles referenced by role.RoleIDs recursively.
func (sn *Snapshot) virtualRoleIDs(roleIDs []string) []string {
	mapRoleIDs := make(map[string]struct{}, len(roleIDs))
	queue := make([]string, 0, len(roleIDs))

	for _, roleID := range roleIDs {
		if _, ok := mapRoleIDs[roleID]; !ok {
			mapRoleIDs[roleID] = struct{}{}
			queue = append(queue, roleID)
		}
	}

	for len(queue) > 0 {
		roleID := queue[0]
		queue = queue[1:]

		role, ok := sn.Roles[roleID]
		if !ok {
			continue
		}

		for _, subID := range role.RoleIDs {
			if _, ok := mapRoleIDs[subID]; !ok {
				mapRoleIDs[subID] = struct{}{}
				queue = append(queue, subID)
			}
		}
	}

	result := make([]string, 0, len(mapRoleIDs))
	for roleID := range mapRoleIDs {
		result = append(result, roleID)
	}
	sort.Strings(result)

	return result
}

func userPermanentRoleSet(u *data.User) map[string]struct{} {
	set := make(map[string]struct{}, len(u.RoleIDs)+len(u.SyncRoleIDs))
	for _, id := range u.RoleIDs {
		set[id] = struct{}{}
	}
	for _, id := range u.SyncRoleIDs {
		set[id] = struct{}{}
	}

	return set
}

func userTmpRoleSet(u *data.User) map[string]struct{} {
	set := map[string]struct{}{}
	now := time.Now()
	for _, tmp := range u.TmpRoleIDs {
		if now.Before(tmp.ExpiresAt.Time) {
			set[tmp.ID] = struct{}{}
		}
	}

	return set
}

func userMixRoleSet(u *data.User) map[string]struct{} {
	set := userPermanentRoleSet(u)
	for _, id := range validIDs(u.TmpRoleIDs) {
		set[id] = struct{}{}
	}

	return set
}

func anyInSet(set map[string]struct{}, ids []string) bool {
	for _, id := range ids {
		if _, ok := set[id]; ok {
			return true
		}
	}

	return false
}

// permissionIDsByRequest returns ids of permissions matching method/path/names.
func (sn *Snapshot) permissionIDsByRequest(method, path string, names []string) []string {
	ids := make([]string, 0)

	for _, id := range sn.PermIDs {
		perm := sn.Permissions[id]

		if (method != "" || path != "") && !permissionMatchesRequest(perm, method, path) {
			continue
		}

		if len(names) > 0 && !matchAnyNameFold(perm.Name, names) {
			continue
		}

		ids = append(ids, id)
	}

	return ids
}

// roleIDsByPermissionIDs returns roles containing any of the given permission ids.
func (sn *Snapshot) roleIDsByPermissionIDs(permIDs []string) []string {
	permSet := make(map[string]struct{}, len(permIDs))
	for _, id := range permIDs {
		permSet[id] = struct{}{}
	}

	roleIDs := make([]string, 0)
	for _, id := range sn.RoleIDs {
		if anyInSet(permSet, sn.Roles[id].PermissionIDs) {
			roleIDs = append(roleIDs, id)
		}
	}

	return roleIDs
}

// ////////////////////////////////////////////////////////////////////
// users

func (sn *Snapshot) UserByAlias(alias string) *data.User {
	if id, ok := sn.Alias[alias]; ok {
		return sn.Users[id]
	}

	return nil
}

func (sn *Snapshot) filterUsers(req data.GetUserRequest) ([]*data.User, error) {
	switch {
	case req.ID != "":
		if user, ok := sn.Users[req.ID]; ok && matchUserFlags(user, req) {
			return []*data.User{user}, nil
		}

		return nil, nil
	case req.Alias != "":
		if user := sn.UserByAlias(req.Alias); user != nil && matchUserFlags(user, req) {
			return []*data.User{user}, nil
		}

		return nil, nil
	}

	roleIDs := req.RoleIDs

	if req.Method != "" || req.Path != "" || len(req.Permissions) > 0 {
		permIDs := sn.permissionIDsByRequest(req.Method, req.Path, req.Permissions)
		if len(permIDs) == 0 {
			return nil, nil
		}

		newRoleIDs := sn.roleIDsByPermissionIDs(permIDs)
		if len(newRoleIDs) == 0 {
			return nil, nil
		}

		roleIDs = append(roleIDs, newRoleIDs...)
	}

	var expandedRoleIDs []string
	if len(roleIDs) > 0 {
		expandedRoleIDs = sn.virtualRoleIDs(roleIDs)
	}

	users := make([]*data.User, 0)
	for _, id := range sn.UserIDs {
		user := sn.Users[id]

		if !matchUserFlags(user, req) {
			continue
		}

		if req.Name != "" && !containsFold(castString(user.Details["name"]), req.Name) {
			continue
		}
		if req.Email != "" && !containsFold(castString(user.Details["email"]), req.Email) {
			continue
		}
		if req.UID != "" && !containsFold(castString(user.Details["uid"]), req.UID) {
			continue
		}

		switch req.RoleType {
		case "PERMANENT":
			permanent := userPermanentRoleSet(user)
			if len(permanent) == 0 {
				continue
			}

			if len(expandedRoleIDs) > 0 && !anyInSet(permanent, expandedRoleIDs) {
				continue
			}
		case "TEMPORARY":
			tmp := userTmpRoleSet(user)
			if len(tmp) == 0 {
				continue
			}

			if len(expandedRoleIDs) > 0 && !anyInSet(tmp, expandedRoleIDs) {
				continue
			}
		default:
			if len(expandedRoleIDs) > 0 && !anyInSet(userMixRoleSet(user), expandedRoleIDs) {
				continue
			}
		}

		users = append(users, user)
	}

	return users, nil
}

func castString(v any) string {
	s, _ := v.(string)

	return s
}

func matchUserFlags(user *data.User, req data.GetUserRequest) bool {
	if req.ServiceAccount != nil && user.ServiceAccount != *req.ServiceAccount {
		return false
	}

	if req.Disabled != nil && user.Disabled != *req.Disabled {
		return false
	}

	if req.LocalUser != nil && user.Local != *req.LocalUser {
		return false
	}

	return true
}

func (c *Cache) GetUsers(req data.GetUserRequest) (*data.Response[[]data.UserExtended], error) {
	sn := c.Snapshot()

	users, err := sn.filterUsers(req)
	if err != nil {
		return nil, err
	}

	count := uint64(len(users))

	if req.Offset > 0 {
		if int(req.Offset) >= len(users) {
			users = nil
		} else {
			users = users[req.Offset:]
		}
	}
	if req.Limit > 0 && int(req.Limit) < len(users) {
		users = users[:req.Limit]
	}

	extended := make([]data.UserExtended, 0, len(users))
	for _, user := range users {
		ext := sn.extendUser(req.AddRoles, req.AddPermissions, req.AddData, req.AddScopeRoles, user)
		ext.IsActive = !user.Disabled
		extended = append(extended, ext)
	}

	return &data.Response[[]data.UserExtended]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: extended,
	}, nil
}

func (c *Cache) GetUser(req data.GetUserRequest) (*data.UserExtended, error) {
	sn := c.Snapshot()

	users, err := sn.filterUsers(req)
	if err != nil {
		return nil, err
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("user %s%s not found; %w", req.ID, req.Alias, data.ErrNotFound)
	}

	user := users[0]

	ext := sn.extendUser(req.AddRoles, req.AddPermissions, req.AddData, req.AddScopeRoles, user)
	ext.IsActive = !user.Disabled

	if req.Sanitize {
		userCopy := *user
		details := make(map[string]any, len(user.Details))
		for k, v := range user.Details {
			details[k] = v
		}

		delete(details, "password")
		delete(details, "secret")

		userCopy.Details = details
		ext.User = &userCopy
	}

	return &ext, nil
}

// extendUser builds UserExtended payload with roles/permissions/data/scope info.
func (sn *Snapshot) extendUser(addRoles, addRolePermissions, addData, addScopeRoles bool, user *data.User) data.UserExtended {
	userExtended := data.UserExtended{User: user}

	if !addRoles && !addRolePermissions && !addData && !addScopeRoles {
		return userExtended
	}

	mapFixedRoleIDs := userPermanentRoleSet(user)

	mapTmpRoleIDs := make(map[string]types.Time, len(user.TmpRoleIDs))
	for _, tmpRole := range validIDsWithTmpID(user.TmpRoleIDs) {
		mapTmpRoleIDs[tmpRole.ID] = tmpRole.ExpiresAt
	}

	mapTmpPermissionIDs := make(map[string]types.Time, len(user.TmpPermissionIDs))
	for _, tmpPermission := range validIDsWithTmpID(user.TmpPermissionIDs) {
		mapTmpPermissionIDs[tmpPermission.ID] = tmpPermission.ExpiresAt
	}

	mapFixedPermissionIDs := make(map[string]struct{}, len(user.PermissionIDs))
	for _, permissionID := range user.PermissionIDs {
		mapFixedPermissionIDs[permissionID] = struct{}{}
	}

	roleIDs := sn.virtualRoleIDs(slicesUnique(user.RoleIDs, user.SyncRoleIDs, validIDs(user.TmpRoleIDs)))

	var roles []data.IDName
	var permissions []data.IDName
	var rolePermissionData []any
	var scope map[string][]string

	permissionIDs := make(map[string]struct{}, 100)

	addPermission := func(permissionID string) {
		if _, ok := permissionIDs[permissionID]; ok {
			return
		}

		permission, ok := sn.Permissions[permissionID]
		if !ok {
			return
		}

		if addRolePermissions {
			permissionAdd := data.IDName{
				ID:   permission.ID,
				Name: permission.Name,
			}

			_, isFixed := mapFixedPermissionIDs[permission.ID]
			_, isTmp := mapTmpPermissionIDs[permission.ID]

			if !isFixed && isTmp {
				expiresAt := mapTmpPermissionIDs[permission.ID]
				permissionAdd.ExpiresAt = &expiresAt
			}

			if !isFixed && !isTmp {
				permissionAdd.Inherited = true
			}

			permissions = append(permissions, permissionAdd)
		}

		if addData && len(permission.Data) > 0 {
			rolePermissionData = append(rolePermissionData, permission.Data)
		}

		if addScopeRoles {
			if scope == nil {
				scope = make(map[string][]string)
			}

			for s, v := range permission.Scope {
				scope[s] = append(scope[s], v...)
			}
		}

		permissionIDs[permissionID] = struct{}{}
	}

	if addRolePermissions || addData || addScopeRoles {
		for _, permissionID := range slicesUnique(user.PermissionIDs, validIDs(user.TmpPermissionIDs)) {
			addPermission(permissionID)
		}
	}

	for _, roleID := range roleIDs {
		role, ok := sn.Roles[roleID]
		if !ok {
			continue
		}

		if addRoles {
			roleAdd := data.IDName{
				ID:   role.ID,
				Name: role.Name,
			}

			_, isFixed := mapFixedRoleIDs[role.ID]
			_, isTmp := mapTmpRoleIDs[role.ID]

			if !isFixed && isTmp {
				expiresAt := mapTmpRoleIDs[role.ID]
				roleAdd.ExpiresAt = &expiresAt
			}

			if !isFixed && !isTmp {
				roleAdd.Inherited = true
			}

			roles = append(roles, roleAdd)
		}

		if addData && len(role.Data) > 0 {
			rolePermissionData = append(rolePermissionData, role.Data)
		}

		if addRolePermissions || addData || addScopeRoles {
			for _, permissionID := range role.PermissionIDs {
				addPermission(permissionID)
			}
		}
	}

	for s, v := range scope {
		scope[s] = slicesUnique(v)
	}

	userExtended.Roles = roles
	userExtended.Permissions = permissions
	userExtended.Data = rolePermissionData

	if addScopeRoles {
		userExtended.Scope = scope
	}

	return userExtended
}

// ////////////////////////////////////////////////////////////////////
// roles

func (sn *Snapshot) extendRole(addRoles, addPermissions, addTotalUsers bool, role *data.Role) data.RoleExtended {
	roleExtended := data.RoleExtended{Role: role}

	if !addRoles {
		return roleExtended
	}

	if addTotalUsers {
		var count uint64
		for _, id := range sn.UserIDs {
			if _, ok := userMixRoleSet(sn.Users[id])[role.ID]; ok {
				count++
			}
		}

		roleExtended.TotalUsers = count
	}

	var roles []data.IDName
	for _, subID := range role.RoleIDs {
		if sub, ok := sn.Roles[subID]; ok {
			roles = append(roles, data.IDName{ID: sub.ID, Name: sub.Name})
		}
	}
	roleExtended.Roles = roles

	if addPermissions {
		var permissions []data.IDName
		for _, permID := range role.PermissionIDs {
			if perm, ok := sn.Permissions[permID]; ok {
				permissions = append(permissions, data.IDName{ID: perm.ID, Name: perm.Name})
			}
		}

		roleExtended.Permissions = permissions
	}

	return roleExtended
}

func (c *Cache) GetRoles(req data.GetRoleRequest) (*data.Response[[]data.RoleExtended], error) {
	sn := c.Snapshot()

	var roles []*data.Role

	if req.ID != "" {
		if role, ok := sn.Roles[req.ID]; ok {
			roles = append(roles, role)
		}
	} else {
		permissionIDs := req.PermissionIDs
		if req.Method != "" || req.Path != "" || len(req.Permissions) > 0 {
			newIDs := sn.permissionIDsByRequest(req.Method, req.Path, req.Permissions)
			if len(newIDs) == 0 {
				return &data.Response[[]data.RoleExtended]{
					Meta:    &data.Meta{Offset: req.Offset, Limit: req.Limit},
					Payload: []data.RoleExtended{},
				}, nil
			}

			permissionIDs = append(permissionIDs, newIDs...)
		}

		permSet := make(map[string]struct{}, len(permissionIDs))
		for _, id := range permissionIDs {
			permSet[id] = struct{}{}
		}

		roleSet := make(map[string]struct{}, len(req.RoleIDs))
		for _, id := range req.RoleIDs {
			roleSet[id] = struct{}{}
		}

		for _, id := range sn.RoleIDs {
			role := sn.Roles[id]

			if req.Name != "" && !containsFold(role.Name, req.Name) {
				continue
			}

			if req.Description != "" && !containsFold(role.Description, req.Description) {
				continue
			}

			if len(permSet) > 0 && !anyInSet(permSet, role.PermissionIDs) {
				continue
			}

			if len(roleSet) > 0 && !anyInSet(roleSet, role.RoleIDs) {
				continue
			}

			roles = append(roles, role)
		}
	}

	count := uint64(len(roles))

	if req.Offset > 0 {
		if int(req.Offset) >= len(roles) {
			roles = nil
		} else {
			roles = roles[req.Offset:]
		}
	}
	if req.Limit > 0 && int(req.Limit) < len(roles) {
		roles = roles[:req.Limit]
	}

	extended := make([]data.RoleExtended, 0, len(roles))
	for _, role := range roles {
		extended = append(extended, sn.extendRole(req.AddRoles, req.AddPermissions, req.AddTotalUsers, role))
	}

	return &data.Response[[]data.RoleExtended]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: extended,
	}, nil
}

func (c *Cache) GetRole(req data.GetRoleRequest) (*data.RoleExtended, error) {
	sn := c.Snapshot()

	role, ok := sn.Roles[req.ID]
	if !ok {
		return nil, fmt.Errorf("role with id %s not found; %w", req.ID, data.ErrNotFound)
	}

	extended := sn.extendRole(req.AddRoles, req.AddPermissions, req.AddTotalUsers, role)

	return &extended, nil
}

// ////////////////////////////////////////////////////////////////////
// permissions

func (sn *Snapshot) extendPermission(addRoles bool, permission *data.Permission) data.PermissionExtended {
	extended := data.PermissionExtended{Permission: permission}

	if !addRoles {
		return extended
	}

	var roles []data.IDName
	for _, id := range sn.RoleIDs {
		role := sn.Roles[id]
		for _, permID := range role.PermissionIDs {
			if permID == permission.ID {
				roles = append(roles, data.IDName{ID: role.ID, Name: role.Name})
				break
			}
		}
	}

	extended.Roles = roles

	return extended
}

func (c *Cache) GetPermissions(req data.GetPermissionRequest) (*data.Response[[]data.PermissionExtended], error) {
	sn := c.Snapshot()

	var permissions []*data.Permission

	if req.ID != "" {
		if permission, ok := sn.Permissions[req.ID]; ok {
			permissions = append(permissions, permission)
		}
	} else {
		for _, id := range sn.PermIDs {
			permission := sn.Permissions[id]

			if req.Name != "" && !containsFold(permission.Name, req.Name) {
				continue
			}

			if req.Description != "" && !containsFold(permission.Description, req.Description) {
				continue
			}

			if (req.Method != "" || req.Path != "") && !permissionMatchesRequest(permission, req.Method, req.Path) {
				continue
			}

			if len(req.Data) > 0 && !matchPermissionData(permission, req.Data) {
				continue
			}

			permissions = append(permissions, permission)
		}
	}

	count := uint64(len(permissions))

	if req.Offset > 0 {
		if int(req.Offset) >= len(permissions) {
			permissions = nil
		} else {
			permissions = permissions[req.Offset:]
		}
	}
	if req.Limit > 0 && int(req.Limit) < len(permissions) {
		permissions = permissions[:req.Limit]
	}

	extended := make([]data.PermissionExtended, 0, len(permissions))
	for _, permission := range permissions {
		extended = append(extended, sn.extendPermission(req.AddRoles, permission))
	}

	return &data.Response[[]data.PermissionExtended]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: extended,
	}, nil
}

func matchPermissionData(permission *data.Permission, filter map[string]string) bool {
	for k, v := range filter {
		if vv, ok := permission.Data[k]; ok {
			if containsFold(fmt.Sprint(vv), v) {
				return true
			}
		}
	}

	return false
}

func (c *Cache) GetPermission(req data.GetPermissionRequest) (*data.PermissionExtended, error) {
	sn := c.Snapshot()

	permission, ok := sn.Permissions[req.ID]
	if !ok {
		return nil, fmt.Errorf("permission with id %s not found; %w", req.ID, data.ErrNotFound)
	}

	extended := sn.extendPermission(req.AddRoles, permission)

	return &extended, nil
}

// ////////////////////////////////////////////////////////////////////
// lmaps

func (c *Cache) GetLMaps(req data.GetLMapRequest) (*data.Response[[]data.LMap], error) {
	sn := c.Snapshot()

	roleSet := make(map[string]struct{}, len(req.RoleIDs))
	for _, id := range req.RoleIDs {
		roleSet[id] = struct{}{}
	}

	lmaps := make([]data.LMap, 0, len(sn.LMapNames))
	for _, name := range sn.LMapNames {
		lmap := sn.LMaps[name]

		if req.Name != "" && lmap.Name != req.Name {
			continue
		}

		if len(roleSet) > 0 && !anyInSet(roleSet, lmap.RoleIDs) {
			continue
		}

		lmaps = append(lmaps, *lmap)
	}

	count := uint64(len(lmaps))

	if req.Offset > 0 {
		if int(req.Offset) >= len(lmaps) {
			lmaps = nil
		} else {
			lmaps = lmaps[req.Offset:]
		}
	}
	if req.Limit > 0 && int(req.Limit) < len(lmaps) {
		lmaps = lmaps[:req.Limit]
	}

	return &data.Response[[]data.LMap]{
		Meta: &data.Meta{
			Offset:         req.Offset,
			Limit:          req.Limit,
			TotalItemCount: count,
		},
		Payload: lmaps,
	}, nil
}

func (c *Cache) GetLMap(name string) (*data.LMap, error) {
	sn := c.Snapshot()

	lmap, ok := sn.LMaps[name]
	if !ok {
		return nil, fmt.Errorf("lmap with name %s not found; %w", name, data.ErrNotFound)
	}

	return lmap, nil
}

// ////////////////////////////////////////////////////////////////////
// check

func (c *Cache) Check(req data.CheckRequest) (*data.CheckResponse, error) {
	sn := c.Snapshot()

	var user *data.User
	if req.ID != "" {
		user = sn.Users[req.ID]
	} else if req.Alias != "" {
		user = sn.UserByAlias(req.Alias)
	}

	if user == nil || user.Disabled {
		return &data.CheckResponse{Allowed: false}, nil
	}

	return sn.checkUser(user, req), nil
}

func (sn *Snapshot) checkUser(user *data.User, req data.CheckRequest) *data.CheckResponse {
	roleIDs := sn.virtualRoleIDs(slicesUnique(user.RoleIDs, user.SyncRoleIDs, validIDs(user.TmpRoleIDs)))

	permissionMapIDs := make(map[string]struct{})
	for _, permID := range user.PermissionIDs {
		permissionMapIDs[permID] = struct{}{}
	}
	for _, permID := range validIDs(user.TmpPermissionIDs) {
		permissionMapIDs[permID] = struct{}{}
	}
	for _, roleID := range roleIDs {
		if role, ok := sn.Roles[roleID]; ok {
			for _, permID := range role.PermissionIDs {
				permissionMapIDs[permID] = struct{}{}
			}
		}
	}

	for permID := range permissionMapIDs {
		perm, ok := sn.Permissions[permID]
		if !ok {
			continue
		}

		if checkAccess(sn.Check, perm, req.Host, req.Path, req.Method) {
			return &data.CheckResponse{Allowed: true}
		}
	}

	return &data.CheckResponse{Allowed: false}
}

// ////////////////////////////////////////////////////////////////////
// dashboard

func (c *Cache) Dashboard() (*data.Dashboard, error) {
	sn := c.Snapshot()

	var totalUsers, totalServiceAccounts uint64
	for _, id := range sn.UserIDs {
		if sn.Users[id].ServiceAccount {
			totalServiceAccounts++
		} else {
			totalUsers++
		}
	}

	extendRoles := make([]data.RoleExtended, 0, len(sn.RoleIDs))
	for _, id := range sn.RoleIDs {
		extendRoles = append(extendRoles, sn.extendRole(true, true, true, sn.Roles[id]))
	}

	return &data.Dashboard{
		Roles:                extendRoles,
		TotalRoles:           uint64(len(sn.RoleIDs)),
		TotalPermissions:     uint64(len(sn.PermIDs)),
		TotalUsers:           totalUsers,
		TotalServiceAccounts: totalServiceAccounts,
	}, nil
}
