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

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/xhit/go-str2duration/v2"
)

var (
	DefaultCachePollInterval                        = 5 * time.Second
	DefaultDisabledNotificationListenerPollInterval = 10 * time.Second
)

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
	// PublicPermIDs are permissions flagged public: their resources are
	// matched for anonymous requests and for every user.
	PublicPermIDs []string

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
	Authorize     AuthorizeSettings
	Audit         AuditSettings
	SCIM          SCIMSettings
	Registration  RegistrationSettings

	// SessionProviders is the UI-managed session middleware provider list
	// ("session_providers" namespace) with every group merged in;
	// read-only, replaced on reload.
	SessionProviders map[string]session.Provider
	// SessionProviderGroups holds the named provider groups of the
	// "session_providers" namespace; read-only, replaced on reload.
	SessionProviderGroups map[string]map[string]session.Provider
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

	authorizeRaw, err := c.store.GetSettingValue(ctx, "authorize")
	if err != nil {
		return fmt.Errorf("load authorize settings: %w", err)
	}

	auditRaw, err := c.store.GetSettingValue(ctx, "audit")
	if err != nil {
		return fmt.Errorf("load audit settings: %w", err)
	}

	scimRaw, err := c.store.GetSettingValue(ctx, "scim")
	if err != nil {
		return fmt.Errorf("load scim settings: %w", err)
	}

	registrationRaw, err := c.store.GetSettingValue(ctx, "registration")
	if err != nil {
		return fmt.Errorf("load registration settings: %w", err)
	}

	sessionProvidersRaw, err := c.store.GetSettingValue(ctx, "session_providers")
	if err != nil {
		return fmt.Errorf("load session_providers settings: %w", err)
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
			snap.Alias[normalizeAlias(alias)] = user.ID
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
		permission.Resources = normalizeResources(permission.Resources)
		snap.Permissions[permission.ID] = permission
		snap.PermIDs = append(snap.PermIDs, permission.ID)
		snap.PermNames[permission.Name] = permission.ID

		if permission.Public {
			snap.PublicPermIDs = append(snap.PublicPermIDs, permission.ID)
		}
	}
	sort.Strings(snap.PermIDs)
	sort.Strings(snap.PublicPermIDs)

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
	if snap.Token.RefreshAbsoluteLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Token.RefreshAbsoluteLifetime); err == nil {
			snap.Token.refreshAbsoluteLifetime = d
		}
	}
	if snap.Token.RememberIdleLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Token.RememberIdleLifetime); err == nil && d > 0 {
			snap.Token.rememberIdleLifetime = d
		}
	}
	if snap.Token.RefreshReuseGrace != "" {
		if d, err := str2duration.ParseDuration(snap.Token.RefreshReuseGrace); err == nil && d > 0 {
			snap.Token.refreshReuseGrace = d
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
	if snap.Passkey.Enrollment.SnoozeDuration != "" {
		if d, err := str2duration.ParseDuration(snap.Passkey.Enrollment.SnoozeDuration); err == nil {
			snap.Passkey.Enrollment.snoozeDuration = d
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

	if authorizeRaw != nil {
		if err := json.Unmarshal(authorizeRaw, &snap.Authorize); err != nil {
			slog.Warn("invalid authorize settings", slog.String("error", err.Error()))
		}
	}
	if snap.Authorize.FlowLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Authorize.FlowLifetime); err == nil {
			snap.Authorize.flowLifetime = d
		}
	}

	if scimRaw != nil {
		if err := json.Unmarshal(scimRaw, &snap.SCIM); err != nil {
			slog.Warn("invalid scim settings", slog.String("error", err.Error()))
		}
	}

	if auditRaw != nil {
		if err := json.Unmarshal(auditRaw, &snap.Audit); err != nil {
			slog.Warn("invalid audit settings", slog.String("error", err.Error()))
		}
		if err := parseAuditSettings(&snap.Audit); err != nil {
			slog.Warn("invalid audit settings", slog.String("error", err.Error()))
		}
	}

	if registrationRaw != nil {
		if err := json.Unmarshal(registrationRaw, &snap.Registration); err != nil {
			slog.Warn("invalid registration settings", slog.String("error", err.Error()))
		}
	}
	if snap.Registration.ClientLifetime != "" {
		if d, err := str2duration.ParseDuration(snap.Registration.ClientLifetime); err == nil {
			snap.Registration.clientLifetime = d
		}
	}

	snap.SessionProviders = map[string]session.Provider{}
	snap.SessionProviderGroups = map[string]map[string]session.Provider{}
	if sessionProvidersRaw != nil {
		var sessionProviders SessionProviderSettings
		if err := json.Unmarshal(sessionProvidersRaw, &sessionProviders); err != nil {
			slog.Warn("invalid session_providers settings", slog.String("error", err.Error()))
		} else {
			for name, provider := range sessionProviders.Providers {
				snap.SessionProviders[name] = provider
			}

			for groupName, group := range sessionProviders.Groups {
				groupProviders := resolveSessionProviderGroup(sessionProviders.Providers, group)
				for name, provider := range group.Providers {
					// PutSetting rejects duplicates; hand-written settings can
					// still carry them, so keep the merge deterministic-ish and
					// make the problem visible.
					if _, ok := snap.SessionProviders[name]; ok {
						slog.Warn("duplicate session provider key",
							slog.String("provider", name), slog.String("group", groupName))
					}
					snap.SessionProviders[name] = provider
				}

				snap.SessionProviderGroups[groupName] = groupProviders
			}
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

type CacheWatchConfig struct {
	DisableNotificationListener bool
	PollInterval                time.Duration
}

func (c *Cache) watchPollInterval(cfg CacheWatchConfig) time.Duration {
	if cfg.PollInterval > 0 {
		return cfg.PollInterval
	}
	if cfg.DisableNotificationListener {
		return DefaultDisabledNotificationListenerPollInterval
	}

	return c.Snapshot().Cache.GetPollInterval()
}

// Watch reloads on PostgreSQL notifications and polls as a durable fallback.
// PollInterval pins polling for one instance; otherwise the interval comes from
// the shared "cache" setting namespace and is applied live.
func (c *Cache) Watch(ctx context.Context, dsn string, cfg CacheWatchConfig) {
	changes := make(chan struct{}, 1)
	if !cfg.DisableNotificationListener {
		go c.listenChanges(ctx, dsn, changes)
	}

	interval := c.watchPollInterval(cfg)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var debounceTimer *time.Timer
	var debounce <-chan time.Time
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	refresh := func() {
		version, err := c.store.Version(ctx)
		if err != nil {
			slog.Error("auth cache version check failed", slog.String("error", err.Error()))

			return
		}

		if version != c.Snapshot().Version {
			if err := c.Reload(ctx); err != nil {
				slog.Error("auth cache reload failed", slog.String("error", err.Error()))
			}
		}

		if newInterval := c.watchPollInterval(cfg); newInterval != interval {
			interval = newInterval
			ticker.Reset(interval)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(authNotificationDebounce)
			} else {
				debounceTimer.Reset(authNotificationDebounce)
			}
			debounce = debounceTimer.C
		case <-debounce:
			debounce = nil
			refresh()
		case <-ticker.C:
			refresh()
		}
	}
}
