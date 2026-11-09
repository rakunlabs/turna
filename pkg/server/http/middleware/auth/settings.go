package auth

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/xhit/go-str2duration/v2"
	"golang.org/x/net/publicsuffix"
)

// AccessClient is the decoded OAuth client config stored in auth_oauth_clients.
type AccessClient struct {
	ClientSecret  string   `json:"client_secret"`
	Scope         []string `json:"scope"`
	WhitelistURLs []string `json:"whitelist_urls"`
	// RolesClaim overrides the global token roles_claim dot path for tokens
	// issued to this client (access token, id_token and userinfo alike).
	// Empty falls back to TokenSettings.GetRolesClaim().
	RolesClaim string `json:"roles_claim"`

	// ClientName is a human readable name shown on the consent page.
	ClientName string `json:"client_name,omitempty"`
	// PostLogoutRedirectURIs are exact-match targets the end session
	// endpoint may redirect to after logout (OIDC RP-Initiated Logout).
	PostLogoutRedirectURIs []string `json:"post_logout_redirect_uris,omitempty"`
	// RequireDPoP refuses token requests without a DPoP proof (RFC 9449).
	RequireDPoP bool `json:"require_dpop,omitempty"`
	// RedirectURIs are exact-match redirect targets (RFC 7591 registration).
	// When set they take precedence over the prefix-matched WhitelistURLs.
	RedirectURIs []string `json:"redirect_uris,omitempty"`
	// Public marks a client that authenticates without a secret; the
	// authorization code flow then requires PKCE.
	Public bool `json:"public,omitempty"`
	// Dynamic marks a client created through RFC 7591 dynamic registration.
	Dynamic bool `json:"dynamic,omitempty"`
	// GrantTypes restricts the grants this client may use. Old dynamic clients
	// without this field are limited to the historic registration defaults.
	GrantTypes []string `json:"grant_types,omitempty"`
	// SkipConsent auto-approves the consent step for trusted first-party
	// clients; the user still has to be logged in.
	SkipConsent bool `json:"skip_consent,omitempty"`
	// Resources are the RFC 8707 resource indicators this client may request.
	// Empty allows any requested resource.
	Resources []string `json:"resources,omitempty"`
	// RegistrationToken is the sha256 hex of the RFC 7592 registration access
	// token for dynamically registered clients.
	RegistrationToken string `json:"registration_token,omitempty"`
	// CreatedAt unix seconds; used to expire dynamic registrations.
	CreatedAt int64 `json:"created_at,omitempty"`
}

func (c AccessClient) allowsGrant(grant string) bool {
	if len(c.GrantTypes) > 0 {
		return slices.Contains(c.GrantTypes, grant)
	}
	if c.Dynamic {
		return grant == "authorization_code" || grant == "refresh_token"
	}
	return true
}

// redirectURIAllowedForClient checks a redirect target against the client's
// registration: exact RedirectURIs first, prefix WhitelistURLs otherwise. If
// neither is configured, any non-empty redirect target is allowed.
func (c AccessClient) redirectURIAllowedForClient(redirectURI string) bool {
	if redirectURI == "" {
		return false
	}

	if len(c.RedirectURIs) > 0 {
		for _, uri := range c.RedirectURIs {
			if uri == redirectURI || loopbackRedirectMatch(uri, redirectURI) {
				return true
			}
		}

		return false
	}

	return redirectAllowed(redirectURI, c.WhitelistURLs)
}

// loopbackRedirectMatch compares http loopback redirect URIs ignoring the
// port: native clients listen on an ephemeral port, so RFC 8252 §7.3 makes
// the authorization server accept any port at request time. Client metadata
// documents (e.g. opencode's) register http://127.0.0.1/mcp/oauth/callback and
// redirect to http://127.0.0.1:19876/mcp/oauth/callback.
func loopbackRedirectMatch(registered, requested string) bool {
	reg, err := url.Parse(registered)
	if err != nil {
		return false
	}

	req, err := url.Parse(requested)
	if err != nil {
		return false
	}

	if reg.Scheme != "http" || req.Scheme != "http" || reg.User != nil || req.User != nil {
		return false
	}

	host := reg.Hostname()
	if !isLoopbackHost(host) || !strings.EqualFold(host, req.Hostname()) {
		return false
	}

	return reg.EscapedPath() == req.EscapedPath() && reg.RawQuery == req.RawQuery && reg.Fragment == "" && req.Fragment == ""
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
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

// SessionProviderSettings is the decoded "session_providers" setting
// namespace: a session middleware provider list managed from the auth UI.
// Map keys are provider names and values use the session middleware's own
// provider model. A session middleware pulls this list with
// `provider_source.auth_middleware` (in-process) or over
// GET /v1/session-providers (remote).
//
// Providers holds the ungrouped (shared) entries; Groups holds named
// subsets that a session middleware can pull selectively with
// `provider_source.group` or GET /v1/session-providers/{group}. Provider
// keys must be unique across the ungrouped list and every group, so the
// merged view is conflict-free.
type SessionProviderSettings struct {
	Providers map[string]session.Provider     `json:"providers"`
	Groups    map[string]SessionProviderGroup `json:"groups,omitempty"`
}

// SessionProviderGroup is one named subset of session providers.
type SessionProviderGroup struct {
	Providers map[string]session.Provider `json:"providers"`
	// Inherit references providers from the ungrouped canonical list and may
	// override presentation-only fields without copying credentials/endpoints.
	Inherit map[string]SessionProviderOverride `json:"inherit,omitempty"`
}

type SessionProviderOverride struct {
	// Hide is a pointer so false can explicitly make a provider visible when
	// the canonical provider is hidden. Nil inherits the canonical value.
	Hide *bool `json:"hide,omitempty"`
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
	// BaseURL is the canonical external origin used for redirect URLs and the
	// token issuer. Instance host replacements produce the effective public
	// origin. Default is the request host.
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
	// PollInterval for fallback version polling between instances. Default 5s.
	PollInterval string `json:"poll_interval"`
	// CodeStore configures the temporary OAuth2 code/state cache. Default database.
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
	// RelatedOrigins are origins on other registrable domains that may use
	// passkeys bound to RPID (WebAuthn Related Origin Requests). They are
	// published at https://{rp_id}/.well-known/webauthn and require an
	// explicit RPID.
	RelatedOrigins []string `json:"related_origins"`
	// Sites provide origin-selected relying parties for unrelated domains that
	// share this Auth database. The root fields above remain the default site.
	Sites []PasskeySiteSettings `json:"sites"`
	// Enrollment controls the optional post-login passkey suggestion shown by
	// login middleware instances backed by this auth issuer.
	Enrollment PasskeyEnrollmentSettings `json:"enrollment"`
}

type PasskeySiteSettings struct {
	Name           string   `json:"name"`
	RPID           string   `json:"rp_id"`
	RPDisplayName  string   `json:"rp_display_name"`
	Origins        []string `json:"origins"`
	RelatedOrigins []string `json:"related_origins"`
}

// PasskeyEnrollmentSettings controls optional passkey adoption after a
// successful interactive login. An empty Methods list allows every method.
type PasskeyEnrollmentSettings struct {
	Enabled              bool     `json:"enabled"`
	Methods              []string `json:"methods"`
	PromptWhenRegistered bool     `json:"prompt_when_registered"`
	// SnoozeDuration is how long "Not now" suppresses the prompt in that
	// browser. Empty defaults to 30 days; 0s prompts again on the next login.
	SnoozeDuration string `json:"snooze_duration"`

	snoozeDuration time.Duration
}

func (s PasskeyEnrollmentSettings) AllowsMethod(method string) bool {
	if len(s.Methods) == 0 {
		return true
	}

	for _, allowed := range s.Methods {
		if allowed == method {
			return true
		}
	}

	return false
}

func (s PasskeyEnrollmentSettings) GetSnoozeDuration() time.Duration {
	if s.SnoozeDuration == "" {
		return 30 * 24 * time.Hour
	}

	return s.snoozeDuration
}

func validatePasskeySettings(setting PasskeySettings) error {
	if err := validateDuration(setting.Enrollment.SnoozeDuration); err != nil {
		return fmt.Errorf("enrollment.snooze_duration: %w", err)
	}

	allowedMethods := map[string]bool{
		session.AuthenticationMethodPassword: true,
		session.AuthenticationMethodCode:     true,
		session.AuthenticationMethodPasskey:  true,
	}
	for _, method := range setting.Enrollment.Methods {
		if !allowedMethods[method] {
			return fmt.Errorf("enrollment.methods contains unsupported method %q", method)
		}
	}

	names := make(map[string]struct{}, len(setting.Sites))
	origins := make(map[string]string)

	if len(setting.RelatedOrigins) > 0 {
		if strings.TrimSpace(setting.RPID) == "" {
			return errors.New("related_origins requires an explicit rp_id: the browser fetches https://{rp_id}/.well-known/webauthn")
		}
		if err := validateRelatedOrigins("related_origins", setting.RPID, setting.RelatedOrigins, "the default relying party", origins); err != nil {
			return err
		}
	}

	for i, site := range setting.Sites {
		name := strings.TrimSpace(site.Name)
		if name == "" {
			return fmt.Errorf("sites[%d].name is required", i)
		}
		if _, exists := names[name]; exists {
			return fmt.Errorf("sites contains duplicate name %q", name)
		}
		names[name] = struct{}{}

		if strings.TrimSpace(site.RPID) == "" {
			return fmt.Errorf("sites[%d].rp_id is required", i)
		}
		if len(site.Origins) == 0 {
			return fmt.Errorf("sites[%d].origins is required", i)
		}
		for _, rawOrigin := range site.Origins {
			origin := strings.TrimSuffix(strings.TrimSpace(rawOrigin), "/")
			parsed, err := url.Parse(origin)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
				return fmt.Errorf("sites[%d] contains invalid origin %q", i, rawOrigin)
			}
			hostname := strings.ToLower(parsed.Hostname())
			rpID := strings.ToLower(strings.TrimSpace(site.RPID))
			if hostname != rpID && (net.ParseIP(rpID) != nil || !strings.HasSuffix(hostname, "."+rpID)) {
				return fmt.Errorf("sites[%d] origin %q is outside rp_id %q", i, rawOrigin, site.RPID)
			}
			if owner, exists := origins[origin]; exists {
				return fmt.Errorf("origin %q is assigned to both %q and %q", origin, owner, name)
			}
			origins[origin] = name
		}

		if len(site.RelatedOrigins) > 0 {
			if err := validateRelatedOrigins(fmt.Sprintf("sites[%d].related_origins", i), site.RPID, site.RelatedOrigins, name, origins); err != nil {
				return err
			}
		}
	}

	return nil
}

// maxRelatedOriginLabels is the number of distinct registrable-domain labels
// browsers accept in a /.well-known/webauthn document. The WebAuthn spec sets
// it as a floor; no shipping client allows more.
const maxRelatedOriginLabels = 5

// validateRelatedOrigins checks one profile's Related Origin Requests list.
// owners maps every origin already claimed by a profile, so an origin can
// select only one relying party.
func validateRelatedOrigins(field, rpID string, related []string, owner string, owners map[string]string) error {
	rpID = strings.ToLower(strings.TrimSpace(rpID))
	if net.ParseIP(rpID) != nil {
		return fmt.Errorf("%s: rp_id %q is an IP address; related origins need a domain", field, rpID)
	}

	labels := make(map[string]struct{})
	for _, raw := range related {
		origin, hostname, err := parsePasskeyOrigin(raw)
		if err != nil {
			return fmt.Errorf("%s contains invalid origin %q", field, raw)
		}
		if hostname == rpID || strings.HasSuffix(hostname, "."+rpID) {
			return fmt.Errorf("%s: %q is already inside rp_id %q; list it under origins instead", field, raw, rpID)
		}
		if previous, exists := owners[origin]; exists {
			return fmt.Errorf("origin %q is assigned to both %q and %q", origin, previous, owner)
		}
		owners[origin] = owner

		labels[relatedOriginLabel(hostname)] = struct{}{}
	}

	if len(labels) > maxRelatedOriginLabels {
		return fmt.Errorf("%s spans %d distinct domain labels; browsers accept at most %d", field, len(labels), maxRelatedOriginLabels)
	}

	return nil
}

// parsePasskeyOrigin normalises a scheme://host[:port] origin and returns it
// with its lower-cased hostname.
func parsePasskeyOrigin(raw string) (string, string, error) {
	origin := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", "", errors.New("invalid origin")
	}

	return origin, strings.ToLower(parsed.Hostname()), nil
}

// relatedOriginLabel is the label a browser counts against its related-origin
// limit: the name just before the public suffix ("shop" for shop.co.uk).
func relatedOriginLabel(hostname string) string {
	domain, err := publicsuffix.EffectiveTLDPlusOne(hostname)
	if err != nil {
		return hostname
	}

	label, _, _ := strings.Cut(domain, ".")

	return label
}

// APIKeySettings is the decoded "api_key" setting namespace.
type APIKeySettings struct {
	// Disabled turns off api key creation and validation.
	Disabled bool `json:"disabled"`
	// SelfService lets any authenticated X-User issue and manage their own
	// keys through /v1/api-keys without admin permission. Admins keep access
	// to that plane regardless of this switch. Default off.
	SelfService bool `json:"self_service"`
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

// AuthorizeSettings is the decoded "authorize" setting namespace. It controls
// the local browser-based authorization code flow (consent screen).
type AuthorizeSettings struct {
	// Disabled turns off the local /oauth2/authorize endpoint.
	Disabled bool `json:"disabled"`
	// FlowLifetime of a pending consent. Default 10m.
	FlowLifetime string `json:"flow_lifetime"`
	// LoginURL is where anonymous browsers are sent (with a ?redirect_path=
	// back reference, matching the login middleware convention). Empty shows
	// an error asking the user to log in first.
	LoginURL string `json:"login_url"`
	// DisableRememberMe hides the remember-me checkbox on the consent screen
	// and ignores the form value, so authorization-code sessions always use
	// the standard refresh lifetime.
	DisableRememberMe bool `json:"disable_remember_me"`
	// AllowPlainPKCE accepts code_challenge_method=plain. Off by default;
	// only S256 is accepted (OAuth 2.1, RFC 9700).
	AllowPlainPKCE bool `json:"allow_plain_pkce"`
	// RequirePAR accepts authorization requests only through a request_uri
	// from the pushed authorization request endpoint (RFC 9126).
	RequirePAR bool `json:"require_par"`

	flowLifetime time.Duration
}

// validateDuration checks an optional duration string setting.
func validateDuration(v string) error {
	if v == "" {
		return nil
	}

	_, err := str2duration.ParseDuration(v)

	return err
}

func validateTokenSettings(setting TokenSettings) error {
	parse := func(name, value string, fallback time.Duration) (time.Duration, error) {
		if value == "" {
			return fallback, nil
		}

		d, err := str2duration.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("%s must be positive", name)
		}

		return d, nil
	}

	if _, err := parse("token_lifetime", setting.TokenLifetime, 15*time.Minute); err != nil {
		return err
	}
	refresh, err := parse("refresh_lifetime", setting.RefreshLifetime, 24*time.Hour)
	if err != nil {
		return err
	}
	absolute, err := parse("refresh_absolute_lifetime", setting.RefreshAbsoluteLifetime, 30*24*time.Hour)
	if err != nil {
		return err
	}
	if absolute < refresh {
		return fmt.Errorf("refresh_absolute_lifetime must be at least refresh_lifetime")
	}
	// a remember idle window longer than the absolute lifetime is simply
	// capped by it, so it is not rejected
	if _, err := parse("remember_idle_lifetime", setting.RememberIdleLifetime, 7*24*time.Hour); err != nil {
		return err
	}
	if _, err := parse("refresh_reuse_grace", setting.RefreshReuseGrace, 0); err != nil {
		return err
	}

	return nil
}

func (a AuthorizeSettings) GetFlowLifetime() time.Duration {
	if a.flowLifetime > 0 {
		return a.flowLifetime
	}

	return 10 * time.Minute
}

// RegistrationSettings is the decoded "registration" setting namespace
// (RFC 7591 dynamic client registration). Registration is anonymous, so it
// is off by default and must be enabled explicitly.
type RegistrationSettings struct {
	// Enabled allows dynamic client registration through /oauth2/register.
	Enabled bool `json:"enabled"`
	// ClientLifetime expires dynamic clients after this duration.
	// Empty keeps them forever.
	ClientLifetime string `json:"client_lifetime"`
	// DefaultScope granted to dynamic clients when they do not request one.
	DefaultScope []string `json:"default_scope"`
	// MaxClients caps the number of stored dynamic clients. Default 1000.
	MaxClients int `json:"max_clients"`
	// ClientMetadataDisabled stops accepting URL client_ids (OAuth Client ID
	// Metadata Documents): discovery no longer advertises
	// client_id_metadata_document_supported and no metadata document is
	// fetched. Clients then fall back to dynamic registration, which needs
	// no outbound access; use it when this instance cannot reach the
	// internet. Stored records with a URL id keep working.
	ClientMetadataDisabled bool `json:"client_metadata_disabled"`

	clientLifetime time.Duration
}

func (r RegistrationSettings) GetClientLifetime() time.Duration {
	return r.clientLifetime
}

func (r RegistrationSettings) GetMaxClients() int {
	if r.MaxClients > 0 {
		return r.MaxClients
	}

	return 1000
}

// TokenExchangeSettings is the decoded "token_exchange" setting namespace (RFC 8693).
type TokenExchangeSettings struct {
	// Disabled turns off the token exchange grant.
	Disabled bool `json:"disabled"`
	// AllowedClients limits which clients may exchange tokens. Empty keeps
	// the grant open to every confidential client.
	AllowedClients []string `json:"allowed_clients"`
}

func (t TokenExchangeSettings) clientAllowed(clientID string) bool {
	return len(t.AllowedClients) == 0 || slices.Contains(t.AllowedClients, clientID)
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

// maxPasswordBytes is bcrypt's input limit; longer passwords cannot be hashed.
const maxPasswordBytes = 72

// passwordLengthError reports a password outside the accepted length range.
func (s SignupSettings) passwordLengthError(password string) string {
	if minLen := s.GetPasswordMinLength(); len(password) < minLen {
		return fmt.Sprintf("password must be at least %d characters", minLen)
	}
	if len(password) > maxPasswordBytes {
		return fmt.Sprintf("password must be at most %d bytes", maxPasswordBytes)
	}

	return ""
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
	// $ssl_client_escaped_cert).
	CertHeader string `json:"cert_header"`
	// CertVerifyHeader carries the proxy's certificate verification result
	// (e.g. "ssl-client-verify" from nginx with $ssl_client_verify).
	CertVerifyHeader string `json:"cert_verify_header"`
	// CertVerifyValue is the exact successful verification value. It defaults
	// to "SUCCESS" when CertVerifyHeader is configured.
	CertVerifyValue string `json:"cert_verify_value"`
	// TrustedProxyCIDRs are immediate peer addresses allowed to assert the
	// certificate and verification headers. X-Forwarded-For is never used.
	TrustedProxyCIDRs []string `json:"trusted_proxy_cidrs"`
}

// TokenSettings is the decoded "token" setting namespace.
type TokenSettings struct {
	TokenLifetime           string `json:"token_lifetime"`
	RefreshLifetime         string `json:"refresh_lifetime"`
	RefreshAbsoluteLifetime string `json:"refresh_absolute_lifetime"`
	// RememberIdleLifetime is the idle window of remembered sessions: each
	// refresh extends the session by this much, capped by
	// RefreshAbsoluteLifetime. Empty defaults to 7 days.
	RememberIdleLifetime string `json:"remember_idle_lifetime"`
	// RefreshReuseGrace enables reuse detection for rotated (remembered)
	// refresh tokens: presenting an already rotated token later than this
	// window revokes the whole session family. Empty disables detection.
	RefreshReuseGrace string `json:"refresh_reuse_grace"`
	// RolesClaim is the dot path where the scope-derived roles are written
	// in the access token, the id_token and the userinfo response. Empty
	// defaults to "roles" (flat top-level array). Use "realm_access.roles"
	// for Keycloak-style nesting, or any dot path such as
	// "resource_access.app.roles". A per-client override in
	// AccessClient.RolesClaim takes precedence when set.
	RolesClaim string `json:"roles_claim"`
	// OIDCStandardClaims limits the id_token and userinfo to the standard
	// claims of the granted scopes (OIDC Core §5.4: profile → name,
	// given_name, family_name, preferred_username; email → email,
	// email_verified). Off keeps the historic behavior of always adding the
	// user's name, email and preferred_username.
	OIDCStandardClaims bool `json:"oidc_standard_claims"`

	tokenLifetime           time.Duration
	refreshLifetime         time.Duration
	refreshAbsoluteLifetime time.Duration
	rememberIdleLifetime    time.Duration
	refreshReuseGrace       time.Duration
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

func (t TokenSettings) GetRefreshAbsoluteLifetime() time.Duration {
	if t.refreshAbsoluteLifetime > 0 {
		return t.refreshAbsoluteLifetime
	}

	return 30 * 24 * time.Hour
}

// GetRememberIdleLifetime returns how long a remembered session may stay idle.
func (t TokenSettings) GetRememberIdleLifetime() time.Duration {
	if t.rememberIdleLifetime > 0 {
		return t.rememberIdleLifetime
	}

	return 7 * 24 * time.Hour
}

// GetRefreshReuseGrace returns the reuse detection window; zero disables it.
func (t TokenSettings) GetRefreshReuseGrace() time.Duration {
	return t.refreshReuseGrace
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
