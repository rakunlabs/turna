package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/into"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

const DefaultPrefixPath = "/auth"

// Database connection pool defaults.
const (
	DefaultMaxOpenConns    = 5
	DefaultMaxIdleConns    = 3
	DefaultConnMaxLifetime = 15 * time.Minute
)

// Auth is a self-contained identity provider with its own UI.
// All runtime settings (oauth2, check, cache, token, providers, clients, ldap)
// live in PostgreSQL and are managed through the API/UI. The static
// configuration is only what is needed to reach that database:
// encryption key, database connection and migration settings.
type Auth struct {
	PrefixPath string     `cfg:"prefix_path"`
	Database   Database   `cfg:"database"`
	Encryption Encryption `cfg:"encryption"`

	db           *sql.DB                 `cfg:"-"`
	cipher       *Cipher                 `cfg:"-"`
	store        *Store                  `cfg:"-"`
	cache        *Cache                  `cfg:"-"`
	uiFS         http.HandlerFunc        `cfg:"-"`
	jwtM         jwtManager              `cfg:"-"`
	code         codeManager             `cfg:"-"`
	codeStore    *oauth2store.StoreCache `cfg:"-"`
	codeStoreCfg CodeStoreSettings       `cfg:"-"`
	codeStoreM   sync.Mutex              `cfg:"-"`
	ldap         ldapManager             `cfg:"-"`
	ldapSyncM    sync.Mutex              `cfg:"-"`
	samlM        samlManager             `cfg:"-"`
	encRotateM   sync.Mutex              `cfg:"-"`
}

type Database struct {
	DSN string `cfg:"dsn" log:"-"`

	// MaxOpenConns default 5; negative for unlimited.
	MaxOpenConns int `cfg:"max_open_conns"`
	// MaxIdleConns default 3; negative for none.
	MaxIdleConns int `cfg:"max_idle_conns"`
	// ConnMaxLifetime default 15m; negative for unlimited.
	ConnMaxLifetime time.Duration `cfg:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `cfg:"conn_max_idle_time"`

	Migration Migration `cfg:"migration"`
}

// Migration runs the embedded SQL migrations on startup.
type Migration struct {
	// DSN used only for running migrations; defaults to database.dsn.
	// Set it to a user with DDL privileges when the runtime user has none.
	DSN string `cfg:"dsn" log:"-"`

	Disabled bool `cfg:"disabled"`

	// Values for muz template substitution inside migration files.
	Values map[string]string `cfg:"values"`

	Table   string `cfg:"table"`
	LockKey string `cfg:"lock_key"`
}

type Encryption struct {
	Key string `cfg:"key" log:"-"`
}

func nowAdd(d time.Duration) int64 {
	return time.Now().Add(d).Unix()
}

func (m *Auth) Middleware(ctx context.Context, name string) (func(http.Handler) http.Handler, error) {
	if m.PrefixPath == "" {
		m.PrefixPath = DefaultPrefixPath
	}
	m.PrefixPath = "/" + strings.Trim(m.PrefixPath, "/")

	if m.Database.DSN == "" {
		return nil, errors.New("auth database dsn is required")
	}

	cipher, err := NewCipher(m.Encryption.Key)
	if err != nil {
		return nil, fmt.Errorf("init auth encryption: %w", err)
	}

	db, err := sql.Open("pgx", m.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("open auth database: %w", err)
	}

	if m.Database.MaxOpenConns == 0 {
		m.Database.MaxOpenConns = DefaultMaxOpenConns
	}
	if m.Database.MaxIdleConns == 0 {
		m.Database.MaxIdleConns = DefaultMaxIdleConns
	}
	if m.Database.ConnMaxLifetime == 0 {
		m.Database.ConnMaxLifetime = DefaultConnMaxLifetime
	}

	db.SetMaxOpenConns(max(m.Database.MaxOpenConns, 0))
	db.SetMaxIdleConns(m.Database.MaxIdleConns)
	db.SetConnMaxLifetime(max(m.Database.ConnMaxLifetime, 0))
	if m.Database.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(m.Database.ConnMaxIdleTime)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping auth database: %w", err)
	}

	if !m.Database.Migration.Disabled {
		if err := m.runMigration(ctx, db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("migrate auth database: %w", err)
		}
	}

	m.db = db
	m.cipher = cipher
	m.store = NewStore(db, cipher)

	// fail fast with a clear message when the configured encryption.key does
	// not match the key that wrote the existing data. Skipped when migrations
	// are disabled because the canary table may not exist yet.
	if !m.Database.Migration.Disabled {
		if err := m.verifyEncryptionKey(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	m.cache = NewCache(m.store)

	if err := m.cache.Reload(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("load auth cache: %w", err)
	}

	if err := m.ensureJWT(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init auth jwt: %w", err)
	}

	// oauth2 code/state store; defaults to memory and can be moved to Redis
	// through the runtime "cache" setting namespace.
	if _, err := m.codeStoreRuntime(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init auth code store: %w", err)
	}
	into.ShutdownAdd(m.closeCodeStore, "auth code store")

	// oauth2 code client settings live in the database ("oauth2" namespace);
	// warm it up here to catch configuration problems early.
	if _, err := m.codeRuntime(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init auth code client: %w", err)
	}

	uiMiddleware, err := m.UIMiddleware()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	m.uiFS = uiMiddleware(nil).ServeHTTP

	into.ShutdownAdd(db.Close, "auth db")

	go m.cache.Watch(ctx)
	go m.watchLDAP(ctx)

	// register as in-process token issuer so session providers can use
	// `auth_middleware: <name>` for keyfunc/refresh without HTTP self-calls.
	session.IssuerRegistry.Set(name, m)

	mux := m.MuxSet(m.PrefixPath)

	return func(next http.Handler) http.Handler {
		mux.NotFound(next.ServeHTTP)

		return mux
	}, nil
}

func (m *Auth) MuxSet(prefix string) *ada.Mux {
	mux := ada.NewMux()
	admin := m.adminOnly
	adminHandler := m.adminOnlyHandler

	mux.GET(prefix+"/v1/info", m.Info)
	mux.GET(prefix+"/v1/capabilities", m.CapabilitiesAPI)

	// settings
	mux.GET(prefix+"/v1/settings", admin(m.ListSettings))
	mux.GET(prefix+"/v1/settings/{namespace}", admin(m.GetSetting))
	mux.PUT(prefix+"/v1/settings/{namespace}", admin(m.PutSetting))
	mux.DELETE(prefix+"/v1/settings/{namespace}", admin(m.DeleteSetting))

	// oauth config
	mux.GET(prefix+"/v1/oauth/clients", admin(m.ListOAuthClients))
	mux.GET(prefix+"/v1/oauth/clients/{id}", admin(m.GetOAuthClient))
	mux.PUT(prefix+"/v1/oauth/clients/{id}", admin(m.PutOAuthClient))
	mux.DELETE(prefix+"/v1/oauth/clients/{id}", admin(m.DeleteOAuthClient))
	mux.GET(prefix+"/v1/oauth/providers", admin(m.ListOAuthProviders))
	mux.GET(prefix+"/v1/oauth/providers/{id}", admin(m.GetOAuthProvider))
	mux.PUT(prefix+"/v1/oauth/providers/{id}", admin(m.PutOAuthProvider))
	mux.DELETE(prefix+"/v1/oauth/providers/{id}", admin(m.DeleteOAuthProvider))

	// ldap config
	mux.GET(prefix+"/v1/ldap/configs", admin(m.ListLDAPConfigs))
	mux.GET(prefix+"/v1/ldap/configs/{id}", admin(m.GetLDAPConfig))
	mux.PUT(prefix+"/v1/ldap/configs/{id}", admin(m.PutLDAPConfig))
	mux.DELETE(prefix+"/v1/ldap/configs/{id}", admin(m.DeleteLDAPConfig))

	// ldap runtime
	mux.GET(prefix+"/v1/ldap/groups", admin(m.LdapGetGroupsAPI))
	mux.GET(prefix+"/v1/ldap/users/{uid}", admin(m.LdapGetUserAPI))
	mux.POST(prefix+"/v1/ldap/sync", admin(m.LdapSyncAPI))
	mux.POST(prefix+"/v1/ldap/sync/{uid}", admin(m.LdapSyncUIDAPI))

	// iam: users
	mux.GET(prefix+"/v1/users", admin(m.GetUsersAPI))
	mux.POST(prefix+"/v1/users", admin(m.CreateUserAPI))
	mux.GET(prefix+"/v1/users/export", admin(m.ExportUsersAPI))
	mux.GET(prefix+"/v1/users/{id}", admin(m.GetUserAPI))
	mux.PUT(prefix+"/v1/users/{id}", admin(m.PutUserAPI))
	mux.PATCH(prefix+"/v1/users/{id}", admin(m.PatchUserAPI))
	mux.DELETE(prefix+"/v1/users/{id}", admin(m.DeleteUserAPI))
	mux.POST(prefix+"/v1/users/{id}/access", admin(m.AccessUserAPI))

	// iam: service accounts
	mux.GET(prefix+"/v1/service-accounts", admin(m.GetServiceAccountsAPI))
	mux.POST(prefix+"/v1/service-accounts", admin(m.CreateServiceAccountAPI))
	mux.GET(prefix+"/v1/service-accounts/export", admin(m.ExportServiceAccountsAPI))
	mux.GET(prefix+"/v1/service-accounts/{id}", admin(m.GetServiceAccountAPI))
	mux.PUT(prefix+"/v1/service-accounts/{id}", admin(m.PutServiceAccountAPI))
	mux.PATCH(prefix+"/v1/service-accounts/{id}", admin(m.PatchServiceAccountAPI))
	mux.DELETE(prefix+"/v1/service-accounts/{id}", admin(m.DeleteServiceAccountAPI))
	mux.POST(prefix+"/v1/service-accounts/{id}/access", admin(m.AccessServiceAccountAPI))

	// iam: roles
	mux.GET(prefix+"/v1/roles", admin(m.GetRolesAPI))
	mux.POST(prefix+"/v1/roles", admin(m.CreateRoleAPI))
	mux.PUT(prefix+"/v1/roles/relation", admin(m.PutRoleRelationAPI))
	mux.GET(prefix+"/v1/roles/relation", admin(m.GetRoleRelationAPI))
	mux.GET(prefix+"/v1/roles/export", admin(m.ExportRolesAPI))
	mux.GET(prefix+"/v1/roles/{id}", admin(m.GetRoleAPI))
	mux.PUT(prefix+"/v1/roles/{id}", admin(m.PutRoleAPI))
	mux.PATCH(prefix+"/v1/roles/{id}", admin(m.PatchRoleAPI))
	mux.DELETE(prefix+"/v1/roles/{id}", admin(m.DeleteRoleAPI))

	// iam: permissions
	mux.GET(prefix+"/v1/permissions", admin(m.GetPermissionsAPI))
	mux.POST(prefix+"/v1/permissions", admin(m.CreatePermissionAPI))
	mux.POST(prefix+"/v1/permissions/bulk", admin(m.CreatePermissionBulkAPI))
	mux.GET(prefix+"/v1/permissions/export", admin(m.ExportPermissionsAPI))
	mux.POST(prefix+"/v1/permissions/keep", admin(m.KeepPermissionBulkAPI))
	mux.GET(prefix+"/v1/permissions/{id}", admin(m.GetPermissionAPI))
	mux.PUT(prefix+"/v1/permissions/{id}", admin(m.PutPermissionAPI))
	mux.PATCH(prefix+"/v1/permissions/{id}", admin(m.PatchPermissionAPI))
	mux.DELETE(prefix+"/v1/permissions/{id}", admin(m.DeletePermissionAPI))

	// iam: lmaps
	mux.GET(prefix+"/v1/lmaps", admin(m.GetLMapsAPI))
	mux.POST(prefix+"/v1/lmaps", admin(m.CreateLMapAPI))
	mux.GET(prefix+"/v1/lmaps/{name}", admin(m.GetLMapAPI))
	mux.PUT(prefix+"/v1/lmaps/{name}", admin(m.PutLMapAPI))
	mux.DELETE(prefix+"/v1/lmaps/{name}", admin(m.DeleteLMapAPI))
	mux.GET(prefix+"/v1/ldap/maps", admin(m.GetLMapsAPI))
	mux.POST(prefix+"/v1/ldap/maps", admin(m.CreateLMapAPI))
	mux.GET(prefix+"/v1/ldap/maps/{name}", admin(m.GetLMapAPI))
	mux.PUT(prefix+"/v1/ldap/maps/{name}", admin(m.PutLMapAPI))
	mux.DELETE(prefix+"/v1/ldap/maps/{name}", admin(m.DeleteLMapAPI))

	// iam: check + dashboard + info
	mux.POST(prefix+"/v1/check", admin(m.CheckAPI))
	mux.POST(prefix+"/check", m.CheckUserAPI)
	mux.GET(prefix+"/info", m.UserInfoAPI)
	mux.GET(prefix+"/v1/dashboard", admin(m.DashboardAPI))
	mux.GET(prefix+"/v1/version", admin(m.VersionAPI))
	mux.POST(prefix+"/v1/sync", admin(m.SyncAPI))

	// jwt signing key
	mux.POST(prefix+"/v1/jwt/rotate", admin(m.RotateJWTAPI))

	// record encryption key rotation (re-encrypts all encrypted columns)
	mux.POST(prefix+"/v1/encryption/rotate", admin(m.RotateEncryptionAPI))

	// passkey management (X-User plane)
	mux.POST(prefix+"/v1/passkey/register", m.PasskeyRegisterAPI)
	mux.GET(prefix+"/v1/passkey/credentials", m.PasskeyCredentialsAPI)
	mux.DELETE(prefix+"/v1/passkey/credentials/{id}", m.PasskeyCredentialDeleteAPI)

	// api key management (legacy X-User plane + principal admin)
	mux.GET(prefix+"/v1/api-keys", admin(m.ListAPIKeysAPI))
	mux.POST(prefix+"/v1/api-keys", admin(m.CreateAPIKeyAPI))
	mux.PATCH(prefix+"/v1/api-keys/{id}", admin(m.UpdateAPIKeyAPI))
	mux.DELETE(prefix+"/v1/api-keys/{id}", admin(m.DeleteAPIKeyAPI))
	mux.GET(prefix+"/v1/api-key-principals", admin(m.ListAPIKeyPrincipalsAPI))
	mux.POST(prefix+"/v1/api-key-principals", admin(m.CreateAPIKeyPrincipalAPI))
	mux.PATCH(prefix+"/v1/api-key-principals/{id}", admin(m.UpdateAPIKeyPrincipalAPI))
	mux.DELETE(prefix+"/v1/api-key-principals/{id}", admin(m.DeleteAPIKeyPrincipalAPI))

	// totp management (X-User plane)
	mux.GET(prefix+"/v1/totp", m.TOTPStatusAPI)
	mux.POST(prefix+"/v1/totp/register", m.TOTPRegisterAPI)
	mux.POST(prefix+"/v1/totp/confirm", m.TOTPConfirmAPI)
	mux.POST(prefix+"/v1/totp/recovery", m.TOTPRecoveryAPI)
	mux.DELETE(prefix+"/v1/totp", m.TOTPDeleteAPI)

	// self-service account (X-User plane)
	mux.GET(prefix+"/v1/me", m.MeAPI)
	mux.POST(prefix+"/v1/me/password", m.MePasswordAPI)

	// device flow approval (X-User plane)
	mux.GET(prefix+"/v1/device/{user_code}", m.DeviceInfoAPI)
	mux.POST(prefix+"/v1/device", m.DeviceApproveAPI)

	// saml config
	mux.GET(prefix+"/v1/saml/providers", admin(m.ListSAMLProviders))
	mux.GET(prefix+"/v1/saml/providers/{id}", admin(m.GetSAMLProvider))
	mux.PUT(prefix+"/v1/saml/providers/{id}", admin(m.PutSAMLProvider))
	mux.DELETE(prefix+"/v1/saml/providers/{id}", admin(m.DeleteSAMLProvider))

	// oauth2 runtime
	mux.GET(prefix+"/oauth2/auth/{provider}", m.APIAuth)
	mux.GET(prefix+"/oauth2/code/{provider}", m.APICodeAuth)
	mux.POST(prefix+"/oauth2/token", m.APIToken)
	mux.POST(prefix+"/oauth2/passkey", m.APIPasskeyToken)
	mux.POST(prefix+"/oauth2/device_authorization", m.APIDeviceAuthorization)
	mux.POST(prefix+"/oauth2/email", m.APIEmailCode)
	// email template preview uses unsaved settings from the management UI.
	mux.POST(prefix+"/v1/email/preview", admin(m.EmailPreviewAPI))
	// self-registration and password reset (public, client-authenticated)
	mux.POST(prefix+"/oauth2/signup", m.APISignup)
	mux.POST(prefix+"/oauth2/signup/verify", m.APISignupVerify)
	mux.POST(prefix+"/oauth2/password-reset", m.APIPasswordReset)
	mux.POST(prefix+"/oauth2/password-reset/confirm", m.APIPasswordResetConfirm)
	mux.POST(prefix+"/oauth2/api-key", m.APIKeyAuthAPI)
	mux.GET(prefix+"/oauth2/api-key", m.APIKeyAuthAPI)
	mux.GET(prefix+"/oauth2/certs", m.APICerts)
	mux.GET(prefix+"/oauth2/userinfo", m.APIUserInfo)
	mux.GET(prefix+"/oauth2/.well-known/openid-configuration", m.APIWellKnown)

	// saml runtime
	mux.GET(prefix+"/saml/{provider}/metadata", m.SAMLMetadata)
	mux.GET(prefix+"/saml/{provider}/login", m.SAMLLogin)
	mux.POST(prefix+"/saml/{provider}/acs", m.SAMLACS)

	// swagger
	redirectSwagger := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, prefix+"/swagger/index.html", http.StatusTemporaryRedirect)
	}
	mux.GET(prefix+"/swagger", admin(redirectSwagger))
	mux.GET(prefix+"/swagger/", admin(redirectSwagger))
	mux.GET(prefix+"/swagger/swagger.json", admin(m.SwaggerDocAPI))
	mux.Handle(prefix+"/swagger/*", adminHandler(m.SwaggerUIHandler()))

	// ui
	redirectUI := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, prefix+"/ui/", http.StatusTemporaryRedirect)
	}
	mux.GET(prefix, redirectUI)
	mux.GET(prefix+"/", redirectUI)
	mux.GET(prefix+"/ui", redirectUI)
	mux.Handle(prefix+"/ui/*", m.uiFS)

	return mux
}
