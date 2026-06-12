# auth

`auth` is the PostgreSQL-backed authentication middleware. It replaces the legacy [`iam`](./iam) + [`oauth2`](./oauth2) stack with one middleware that serves IAM, OAuth2, LDAP sync, and an embedded management UI.

The middleware behaves like a standalone app with its own UI: every runtime setting (OAuth2 redirect behavior, permission check rules, cache polling, token lifetimes, OAuth clients/providers, LDAP) lives in PostgreSQL and is managed through the API or UI. The static configuration only covers how to reach that database: encryption key, database connection, and migration settings.

Reads are served from an in-memory read model; writes go to PostgreSQL inside a transaction that bumps a version, records an event, and emits `pg_notify('auth_changed', version)`. Other instances pick up changes through version polling.

```yaml
server:
  http:
    middlewares:
      auth:
        auth:
          prefix_path: /auth
          database:
            dsn: postgres://turna:turna@localhost:5432/turna?sslmode=disable
          encryption:
            key: ${AUTH_ENCRYPTION_KEY}
```

## Fields

| Field | Description |
| --- | --- |
| `prefix_path` | Base path for Auth API and UI. Defaults to `/auth`. |
| `database.dsn` | PostgreSQL connection string. Required. |
| `database.max_open_conns` | `database/sql` max open connection limit. Defaults to `5`; negative for unlimited. |
| `database.max_idle_conns` | `database/sql` max idle connection limit. Defaults to `3`; negative for none. |
| `database.conn_max_lifetime` | Connection max lifetime. Defaults to `15m`; negative for unlimited. |
| `database.conn_max_idle_time` | Optional connection max idle time. |
| `database.migration.dsn` | Optional DSN used only while running migrations (e.g. a user with DDL privileges). Defaults to `database.dsn`; the connection is closed after migration. |
| `database.migration.disabled` | Disable built-in migration bootstrap. |
| `database.migration.values` | Optional `muz.Migrate` template substitution values. |
| `database.migration.table` | Migration tracking table. Defaults to `auth_migrations`. |
| `database.migration.lock_key` | PostgreSQL advisory lock key. Defaults to `muz:postgres:turna:auth_migrations`. |
| `encryption.key` | Required encryption key for secrets stored in PostgreSQL. Raw strings are SHA-256 derived; base64 16/24/32-byte keys are used directly. |

## Runtime settings (stored in PostgreSQL)

Everything else is a settings namespace under `/auth/v1/settings/{namespace}` and takes effect without a restart. The UI exposes dedicated pages for OAuth2, access checks, API keys, email and mTLS; the *Runtime Settings* page keeps the remaining operational namespaces.

| Namespace | Keys | Description |
| --- | --- | --- |
| `admin` | `permission`, `allow_missing_x_user` | Management API/UI authorization. Empty `permission` keeps bootstrap-open behavior. When set, `X-User` must have that permission ID/name. `allow_missing_x_user` defaults to `true` for break-glass access when the session chain is removed and no `X-User` is present. Do not expose this route publicly when break-glass is enabled. |
| `oauth2` | `base_url`, `schema`, `insecure_skip_verify` | Code-flow redirect behavior for upstream providers. `schema` defaults to `https`. |
| `check` | `default_hosts`, `no_host_check` | Host rules for permission evaluation. |
| `cache` | `poll_interval`, `code_store` | Version poll interval for the in-memory read model and OAuth2 temporary code/state store. `code_store.active` is `memory` or `redis`. |
| `token` | `token_lifetime`, `refresh_lifetime` | Token lifetimes (default `15m` / `24h`). |
| `jwt` | `kid`, `private_key` | RS256 signing key (PEM, PKCS#8 or PKCS#1); auto-generated on first start. Editable through the API/UI and applied without restart — the public JWKS key is derived from the private key. Changing or rotating the key invalidates outstanding tokens. |
| `passkey` | `disabled`, `rp_id`, `rp_display_name`, `origins`, `user_verification` | WebAuthn (passkey) relying party settings. Empty `rp_id`/`origins` are derived from the request host and forwarded scheme. |
| `password` | `disabled`, `local_disabled`, `ldap_disabled`, `ldap_register_disabled` | Password grant sources. Defaults keep the implicit behavior: local users check bcrypt, non-local users bind against LDAP, unknown aliases are auto-created from LDAP on first login. |
| `api_key` | `disabled`, `max_lifetime` | Static API key creation and validation. `max_lifetime` caps the expiry of new keys (duration string); empty means keys may live forever. |
| `device` | `disabled`, `code_lifetime`, `interval`, `verification_uri` | RFC 8628 device flow. Defaults: codes live `10m`, minimum poll interval `5` seconds, verification URI `<prefix>/ui/device`. |
| `token_exchange` | `disabled` | RFC 8693 token exchange grant. |
| `totp` | `disabled`, `issuer`, `skew` | TOTP second factor. `issuer` is shown in authenticator apps (default `Turna Auth`), `skew` is the allowed period drift (default `1` = ±30s). Confirming TOTP also issues 8 single-use recovery codes. |
| `email` | `disabled`, `magic_link`, `from`, `subject`, `body_template`, `magic_link_subject`, `magic_link_body_template`, `code_lifetime`, `smtp.{host,port,username,password,no_auth,starttls,tls,insecure_skip_verify}` | Passwordless email login with two independent mails: the one-time code (`disabled`, `subject`, `body_template`) and the magic link (`magic_link` default true, `magic_link_subject`, `magic_link_body_template`). All templates are Go `text/template` strings; empty uses built-in defaults. Set `smtp.no_auth=true` for trusted relays that need no authentication. Codes live `15m` by default. The relay is also used by `signup`. Login is effectively off until `smtp.host` is set. |
| `signup` | `enabled`, `email_verification`, `password_reset`, `default_role_ids`, `code_lifetime`, `verify_subject`, `verify_body_template`, `reset_subject`, `reset_body_template` | Self-registration and forgot-password flows (UI: *Signup*). Off by default. `email_verification` defaults to `true`; verification/reset mails use the `email` SMTP relay. Codes live `1h` by default. Templates are Go `text/template` strings validated on save. |
| `mtls` | `enabled`, `cert_header` | Certificate based client authentication (RFC 8705 style). `cert_header` names a trusted proxy header carrying the client certificate (e.g. nginx `$ssl_client_escaped_cert`); only set it behind a trusted proxy. Off by default. |
| `saml` | `certificate`, `private_key` | SAML SP signing key pair; auto-generated (self-signed, 10 years) on first SAML use. |

Example:

```sh
curl -X PUT /auth/v1/settings/oauth2 -d '{"value":{"schema":"https","base_url":""}}'
curl -X PUT /auth/v1/settings/check -d '{"value":{"no_host_check":true}}'
curl -X PUT /auth/v1/settings/cache -d '{"value":{"poll_interval":"5s","code_store":{"active":"redis","redis":{"address":["redis:6379"]}}}}'
```

## Storage

Migrations are embedded and run through `github.com/rakunlabs/muz` with a PostgreSQL advisory lock. The schema includes:

- `auth_versions`, `auth_events` — monotonic change version and durable event log.
- `auth_settings` — encrypted JSON settings namespaces. Reserved: `admin`, `jwt`, `token`, `oauth2`, `check`, `cache`, `api_key`, `device`, `token_exchange`, `totp`, `email`, `signup`, `mtls`, `saml` (see [Runtime settings](#runtime-settings-stored-in-postgresql)).
- `auth_oauth_clients`, `auth_oauth_providers`, `auth_ldap_configs`, `auth_saml_providers` — encrypted config records.
- `auth_users` — IAM users; the `details` map is encrypted at rest, passwords are bcrypt hashed.
- `auth_roles`, `auth_permissions`, `auth_lmaps` — IAM model.
- `auth_api_keys` — api keys (sha256 hashes; the key itself is never stored).
- `auth_totp_secrets` — encrypted TOTP shared secrets.
- `auth_flow_codes` — short-lived flow state shared between instances (device codes, email login codes, SAML relay states).

## Routes

With `prefix_path: /auth`:

### Management UI

| Route | Purpose |
| --- | --- |
| `/auth/ui/*` | Embedded Svelte management UI (users, roles, permissions, LDAP, OAuth, settings, API keys, email, mTLS, self-service account). |
| `/auth/ui/#account` | Self-service account page for the current `X-User`: password, TOTP recovery, passkeys, roles and permissions. |
| `/auth/ui/#api-keys` | Admin API key principal management: choose an owner user/service account, create with a lifetime, attach role/permission IDs, copy the one-time key, list/update/revoke existing keys, and edit API key runtime settings. |
| `/auth/ui/#email` | Email code login settings: SMTP relay, code mail Go-template subject/body editor and preview. |
| `/auth/ui/#magic-link` | Magic link login settings: enable toggle, magic link mail template editor and preview (shares the `email` SMTP relay). |
| `/auth/ui/#signup` | Self-registration settings: signup/verification/password-reset toggles, default roles, code lifetime, mail template editors with preview. |
| `/auth/ui/device?user_code=XXXX-XXXX` | RFC 8628 device approval/deny page; `user_code` is optional and pre-fills the form when present. |
| `/auth/ui/#mtls` | Global mTLS settings and workflow guide; certificate bindings live on service account records. |
| `/auth/swagger/*` | Swagger UI for the auth API (served with the ada swagger handler; spec at `/auth/swagger/swagger.json`). |

### IAM

| Method | Route | Purpose |
| --- | --- | --- |
| `GET/POST` | `/auth/v1/users` | List/create users. |
| `GET/PUT/PATCH/DELETE` | `/auth/v1/users/{id}` | Manage one user. |
| `POST` | `/auth/v1/users/{id}/access` | Grant/remove temporary roles or permissions. |
| `GET/POST` | `/auth/v1/service-accounts` | List/create service accounts. |
| `GET/PUT/PATCH/DELETE` | `/auth/v1/service-accounts/{id}` | Manage one service account. |
| `POST` | `/auth/v1/service-accounts/{id}/access` | Temporary access for service accounts. |
| `GET/POST` | `/auth/v1/roles` | List/create roles. |
| `GET/PUT/PATCH/DELETE` | `/auth/v1/roles/{id}` | Manage one role. |
| `GET/POST` | `/auth/v1/permissions` | List/create permissions. |
| `GET/PUT/PATCH/DELETE` | `/auth/v1/permissions/{id}` | Manage one permission. |
| `GET/POST` | `/auth/v1/lmaps` | List/create LDAP maps. |
| `GET/PUT/DELETE` | `/auth/v1/lmaps/{name}` | Manage one LDAP map. |
| `POST` | `/auth/v1/check` | Permission check by alias/id + host/path/method. |
| `POST` | `/auth/check` | Permission check for the `X-User` header identity. |
| `GET` | `/auth/info` | Identity info for the `X-User` header. |
| `GET` | `/auth/v1/dashboard` | Totals and extended roles. |

List endpoints parse the query string with [`rakunlabs/query`](https://pkg.go.dev/github.com/rakunlabs/query): use `_limit`/`_offset` for paging (legacy `limit`/`offset` keys still work) plus field filters such as `name=...`, `role_ids=...`, `add_roles=true`.

### LDAP

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/auth/v1/ldap/groups` | List groups from the active LDAP. |
| `GET` | `/auth/v1/ldap/users/{uid}` | Fetch one LDAP user. |
| `POST` | `/auth/v1/ldap/sync` | Sync all LDAP groups/users. |
| `POST` | `/auth/v1/ldap/sync/{uid}` | Sync one LDAP user. |

The active LDAP config is the first enabled record under `/auth/v1/ldap/configs`. A background loop syncs on `sync_duration` (default `10m`) unless `disable_sync` is set.

Automatic group mapping (same model as the legacy `iam` middleware):

1. On every sync, each LDAP group missing from the group maps gets a **role with the same name** (created when missing) and a group map (`lmap`) pointing to that role.
2. Group members receive all roles mapped to their groups as **sync roles** (`sync_role_ids`); these are managed by sync and reset on every run.
3. Users that left all LDAP groups have their sync roles cleared. Local users and service accounts are untouched.
4. Unknown group members are created as non-local users with details (email, uid, name) pulled from LDAP.

Attach more roles to an LDAP group by editing its group map. The management UI shows live LDAP groups with their member counts and mapped roles under *LDAP → Group Maps*.

### OAuth2

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/auth/oauth2/auth/{provider}` | Start authorization code flow against an upstream provider. |
| `GET` | `/auth/oauth2/code/{provider}` | Provider callback; issues a local code. |
| `POST` | `/auth/oauth2/token` | Token endpoint: `password`, `client_credentials`, `refresh_token`, `authorization_code` (PKCE supported), `urn:ietf:params:oauth:grant-type:device_code`, `urn:ietf:params:oauth:grant-type:token-exchange`, `email_code`. |
| `POST` | `/auth/oauth2/passkey` | WebAuthn login: begin without `assertion`, finish with `session_id` + `assertion`; finish issues tokens like the token endpoint. |
| `POST` | `/auth/oauth2/device_authorization` | RFC 8628 device authorization endpoint; returns `device_code`, `user_code`, `verification_uri`. |
| `POST` | `/auth/oauth2/email` | Request a one-time email login code (and magic link when `redirect_uri` is given); always answers 200 to avoid account enumeration. |
| `POST` | `/auth/oauth2/signup` | Self-registration (requires the `signup` setting and valid client credentials). With email verification the response is always generic and the account is created at verify time. |
| `POST` | `/auth/oauth2/signup/verify` | Confirm a signup verification code; creates the local user with the signup default roles. Codes are single use. |
| `POST` | `/auth/oauth2/password-reset` | Request a password reset mail for local users; always answers 200 to avoid account enumeration. |
| `POST` | `/auth/oauth2/password-reset/confirm` | Set a new password with a valid reset code (single use, min 8 chars). |
| `POST`/`GET` | `/auth/oauth2/api-key` | Validate a static API key (`X-API-Key` header or `api_key` form value) and return identity claims for its principal; no token is issued. |
| `GET` | `/auth/oauth2/certs` | JWKS for the auto-generated RS256 signing key. |
| `GET` | `/auth/oauth2/userinfo` | Userinfo for a bearer access token. |
| `GET` | `/auth/oauth2/.well-known/openid-configuration` | OpenID configuration built from the request host. |

Token notes:

- `client_credentials` authenticates service accounts via the `secret` detail. With the `mtls` setting enabled and no secret provided, a client certificate (TLS handshake or trusted proxy header) is matched against the service account's `cert_fingerprint` (sha256 of the DER cert) or `cert_subject` detail.
- `password` checks local users with bcrypt, or LDAP when the user is not `local`. Unknown LDAP users are created on first successful sync.
- The `password` settings namespace makes those sources explicit and switchable: `disabled` rejects the grant entirely, `local_disabled`/`ldap_disabled` block one source, `ldap_register_disabled` stops auto-creating unknown users from LDAP. Managed in the UI under *OAuth2 → Password Login*.
- Users with a confirmed TOTP secret must send a `totp` form field on the password grant; a missing code answers `401` with `error=mfa_required`. A single-use recovery code is accepted in place of the TOTP code.
- OAuth clients come from `/auth/v1/oauth/clients`; service accounts work as a client fallback.
- Token lifetimes come from the `token` settings namespace (default `15m` / `24h`).
- An `id_token` is issued whenever the granted scope contains `openid`; the `nonce` from the authorization request is embedded for code-flow logins.
- **PKCE (RFC 7636):** `/auth/oauth2/auth/{provider}` and `/auth/saml/{provider}/login` accept `code_challenge` (+ `code_challenge_method`, `S256` or `plain`); the `authorization_code` grant then requires a matching `code_verifier`. With a valid verifier public clients (no stored secret) may exchange codes without a client secret.
- **Redirect whitelist:** authorization and SAML login requests validate `redirect_uri` against the client's `whitelist_urls` (prefix match). Pass `client_id` to pin a specific client; without one the URI must match some client's whitelist when at least one is configured. Whitelist-free setups stay open for backwards compatibility.

### Claim mapping (OAuth2/SAML → roles)

OAuth provider and SAML provider records take an optional `claim_mapping`, mirroring the LDAP group-sync model:

```json
{
  "claim_mapping": {
    "roles_claim": "realm_access.roles",
    "use_lmap": true,
    "role_map": {"idp-admin": ["admin"]},
    "register": true
  }
}
```

- `roles_claim` — claim holding group/role values; OAuth2 supports dot paths into nested claims (`realm_access.roles`, `groups`), SAML matches the attribute name or friendly name.
- `use_lmap` — resolve claim values through the LDAP group maps (`lmaps`), sharing one group→role model across LDAP, OAuth2 and SAML.
- `role_map` — map claim values directly to role names or IDs.
- `register` — create unknown users on first login (non-local, like LDAP) with details pulled from the claims.

Mapped roles land in `sync_role_ids` and are managed by the provider on every login (dropped at the IdP ⇒ dropped here); manually assigned roles stay untouched. Avoid pointing LDAP sync and a claim mapping at the same users — both manage the same `sync_role_ids`.

### Self-service account (X-User plane)

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/auth/v1/me` | Own profile: sanitized details, role/permission names, `local` flag, TOTP/passkey/api-key overview. |
| `POST` | `/auth/v1/me/password` | Change own password (`current_password` + `new_password`, min 8 chars). Local users only; verified against the stored bcrypt hash. |
| `GET` | `/auth/info` | Lighter identity info (details, roles, permissions) — predates `/v1/me`. |

Together with the other X-User plane routes (`/v1/passkey/*`, `/v1/totp*`, `/v1/device`), this gives users self-service over interactive credentials. Recovery codes: `POST /auth/v1/totp/recovery` regenerates the set (old codes become invalid). API keys are managed from the admin *System -> API Keys* page because they are machine principals with explicit owners.

### Device flow (RFC 8628)

For CLIs, TVs and other browserless clients:

1. The device posts `client_id` (public clients need no secret) to `/auth/oauth2/device_authorization` and shows `user_code` + `verification_uri` to the user.
2. The user, authenticated elsewhere (session sets `X-User`), approves with `POST /auth/v1/device {"user_code":"XXXX-XXXX"}` (or `"action":"deny"`); `GET /auth/v1/device/{user_code}` shows the pending request for a consent page.
3. The device polls the token endpoint with `grant_type=urn:ietf:params:oauth:grant-type:device_code`. Standard errors apply: `authorization_pending`, `slow_down`, `expired_token`, `access_denied`.

### API keys

Static long-lived credentials for scripts and integrations, managed as machine principals with an explicit owner user/service account. There is no token exchange: the raw key is sent on every request and validated against the database each time, so revocation is immediate.

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/auth/v1/api-key-principals` | Create a key for `user_id` (`name`, optional `expires_in`, `role_ids`, `permission_ids`, `details`). The key (`tak_...`) is returned exactly once; only the sha256 hash is stored. If role/permission fields are omitted, the owner's current effective access is snapshotted; if present, requested IDs must be assigned to the owner. |
| `GET` | `/auth/v1/api-key-principals` | List all key principals (metadata, owner, role/permission IDs; never the raw key). Optional `user_id` filters to one owner. |
| `PATCH` | `/auth/v1/api-key-principals/{id}` | Update name, `role_ids`, `permission_ids`, `details` or `disabled`. Changes apply on the next request. |
| `DELETE` | `/auth/v1/api-key-principals/{id}` | Revoke a key; access stops immediately. |
| `GET/POST/PATCH/DELETE` | `/auth/v1/api-keys...` | Legacy X-User owner-scoped API kept for compatibility and now admin-gated; the management UI uses `api-key-principals`. |

Each key is its own principal: identity claims carry `sub`/`preferred_username` as `api-key:<id>`, `principal_type=api_key`, `api_key_id`, `owner_user_id`, plus the key's own `roles` and `permissions` (IDs and names). `POST /auth/v1/check` accepts `api-key:<id>` as alias. Validation endpoint: `POST /auth/oauth2/api-key` with the `X-API-Key` header returns the claims JSON (`401` when the key is unknown, disabled, expired, or its owner is disabled).

Session integration: set `api_key: true` on the `session` provider. Session validates `X-API-Key` directly — in-process when the provider uses `auth_middleware`, or over `oauth2.api_key_url` against a remote auth instance — deletes the raw key header, and forwards the claims context and `X-User: api-key:<id>` downstream.

### Token exchange (RFC 8693)

Confidential clients can exchange a valid access token for a fresh one with `grant_type=urn:ietf:params:oauth:grant-type:token-exchange` and `subject_token=<jwt>`. The optional `scope` parameter narrows the new token; the response carries `issued_token_type`. Refresh tokens are not exchangeable.

### TOTP (second factor)

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/auth/v1/totp` | Whether the `X-User` has a confirmed secret. |
| `POST` | `/auth/v1/totp/register` | Generate a fresh secret; returns the base32 secret and an `otpauth://` URL. |
| `POST` | `/auth/v1/totp/confirm` | Verify a code and activate enforcement. |
| `DELETE` | `/auth/v1/totp` | Remove the secret. |

Once confirmed, the password grant requires the `totp` field (see token notes). RFC 6238, SHA1/6 digits/30s — compatible with Google Authenticator and friends.

### Email login (one-time code + magic link)

Configure the `email` settings namespace (SMTP relay and templates), then `POST /auth/oauth2/email` with `username=<email>` and client credentials. Exchange with `grant_type=email_code` + `code=...` — codes are single use and unknown addresses are never revealed.

The one-time code and the magic link are two independent mails:

- **One-time code:** controlled by `disabled` with its own `subject` / `body_template`. Sent whenever email login is enabled.
- **Magic link:** controlled by `magic_link` (default `true`) with its own `magic_link_subject` / `magic_link_body_template`. Only produced when the request carries a `redirect_uri` allowed by the client's `whitelist_urls`; the link is `redirect_uri?code=...`. Turn `magic_link` off to send the code only — useful when the same SMTP relay is shared with the `signup` flows and you don't want login magic links.

All subject/body values are Go `text/template`. Available fields: `.Email`, `.Name`, `.Code`, `.MagicLink`, `.ExpiresIn`, `.ClientID`, `.RedirectURI`, `.UserID`, `.UserAlias`. `POST /auth/v1/email/preview` renders an unsaved settings payload (with or without `redirect_uri`) so the UI can preview and validate either mail before saving.

### Signup and password reset

The `signup` settings namespace enables optional self-registration and forgot-password flows, all managed from the UI:

1. `POST /auth/oauth2/signup` with client credentials, `email`, `password` (min 8) and optional `name`/`redirect_uri`. With `email_verification` (default) the pending registration is stored as a hashed flow code — the password is kept bcrypt-hashed, never plain — and a verification mail is sent; the response never reveals whether the address is registered. Without verification the local user is created immediately (duplicates answer `409`).
2. `POST /auth/oauth2/signup/verify` with the mailed `code` creates the user: `local: true`, alias = email, `default_role_ids` granted.
3. `POST /auth/oauth2/password-reset` with `email` mails a reset code to local users (always `200`); `POST /auth/oauth2/password-reset/confirm` with `code` + `password` sets the new password. Non-local (LDAP/federated) users are skipped — their password lives upstream.

Mails use the `email` SMTP relay; magic links are appended to `redirect_uri` when it passes the client's `whitelist_urls`. Verify/reset subject and body templates are Go `text/template` strings (same fields as email login plus `.Name`), validated on save and previewable in the UI. The [`login`](./login) middleware picks these flows up automatically and shows "Create account" / "Forgot password?" on the login page.

### SAML 2.0 (service provider)

SAML IdPs (ADFS, Azure AD, Okta, Shibboleth, ...) are stored like other encrypted config records:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET/PUT/DELETE` | `/auth/v1/saml/providers`, `/auth/v1/saml/providers/{id}` | SAML provider records. |
| `GET` | `/auth/saml/{provider}/metadata` | SP metadata XML to register at the IdP. |
| `GET` | `/auth/saml/{provider}/login` | Start a login; `redirect_uri` required, `state`/`scope` optional. |
| `POST` | `/auth/saml/{provider}/acs` | Assertion consumer service (IdP POST binding callback). |

Provider config keys: `metadata_url` or inline `metadata_xml`, optional `entity_id`, `alias_attribute` (default: email-like attributes, then the subject NameID) and `sign_requests` (RSA-SHA256 with the auto-generated SP key from the `saml` settings namespace). After the assertion is validated the user is redirected to `redirect_uri?code=...&state=...` and the client exchanges the code with the standard `authorization_code` grant — same shape as the upstream OAuth2 provider flow.

### mTLS client credentials

mTLS uses service accounts as clients:

1. Enable the `mtls` setting namespace (`enabled: true`). If TLS terminates at a proxy, set `cert_header` to the trusted header carrying the URL-escaped PEM/base64 DER certificate.
2. Create or edit a service account. Its first alias is the OAuth2 `client_id`.
3. Fill `details.cert_fingerprint` (sha256 DER hex, recommended) or `details.cert_subject` on that service account. The UI can calculate the fingerprint from a pasted PEM certificate.
4. Request `/auth/oauth2/token` with `grant_type=client_credentials` and `client_id=<alias>` while presenting the certificate. A client secret is not required for mTLS-only clients.

Session integration is token based: mTLS authenticates the token request, not the session middleware directly. Use the issued access token as a bearer token on routes protected by `session`.

### Encrypted config

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/auth/v1/info` | Prefix, storage type, and current auth version. |
| `GET` | `/auth/v1/capabilities` | Current request capabilities (`is_admin`, `anonymous_admin`, configured admin permission). The UI uses this to hide admin pages from normal users. |
| `GET/PUT/DELETE` | `/auth/v1/settings`, `/auth/v1/settings/{namespace}` | Encrypted JSON settings. Writes apply immediately on the handling instance; the `jwt` namespace is validated (parseable private key + kid) before saving. |
| `POST` | `/auth/v1/jwt/rotate` | Generate and activate a fresh RSA signing key (new `kid`); outstanding tokens become invalid. |
| `GET/PUT/DELETE` | `/auth/v1/oauth/clients`, `/auth/v1/oauth/clients/{id}` | OAuth client records. |
| `GET/PUT/DELETE` | `/auth/v1/oauth/providers`, `/auth/v1/oauth/providers/{id}` | OAuth provider records. |
| `GET/PUT/DELETE` | `/auth/v1/ldap/configs`, `/auth/v1/ldap/configs/{id}` | LDAP config records. |

Config example:

```json
{
  "enabled": true,
  "config": {
    "client_id": "turna",
    "client_secret": "secret",
    "scopes": ["openid"]
  }
}
```

### Passkeys (WebAuthn)

Passkey support uses the dependency-free engine from `github.com/rakunlabs/ada/middleware/auth/strategy/passkey`. Credentials are stored in `auth_passkey_credentials`; in-flight challenges use the OAuth2 code store (`cache.code_store`), so multi-instance deployments should switch it to Redis.

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/auth/v1/passkey/register` | Begin/finish registration for the `X-User` identity (begin without `credential`, finish with `session_id` + `credential`). |
| `GET` | `/auth/v1/passkey/credentials` | List passkeys for the `X-User` identity. Listing another `user_id` requires admin capability. |
| `DELETE` | `/auth/v1/passkey/credentials/{id}` | Delete a stored passkey. |
| `POST` | `/auth/oauth2/passkey` | Public login ceremony; finish responds with the standard token JSON. |

Login requests carry `client_id`/`client_secret` like the password grant; `username` scopes `allowCredentials` to a known user, empty uses the discoverable (passwordless) flow. The management UI shows registered passkeys on the user page and can enroll a passkey for the operator's own account.

## Session/login integration

The middleware registers itself as an in-process token issuer under its middleware name. A [`session`](./session) provider can reference it with `auth_middleware` so JWT validation (keyfunc) and refresh happen in-process — no `cert_url`/`token_url` self-calls:

```yaml
middlewares:
  auth:
    auth:
      database:
        dsn: postgres://...
      encryption:
        key: ${AUTH_ENCRYPTION_KEY}
  session:
    session:
      store:
        active: redis
        redis:
          address: "redis:6379"
      provider:
        turna:
          auth_middleware: "auth"   # middleware key above
          password_flow: true
          passkey: true
          oauth2:
            client_id: "ui"         # OAuth client registered in auth
            scopes: ["openid"]
      action:
        token:
          login_path: "/login/"
  login:
    login:
      path:
        base: "/login/"
      session_middleware: "session"
routers:
  login:
    path: /login/*
    middlewares: [login]
  auth:
    path: /auth/*
    middlewares: [session, auth]   # session sets X-User for the auth API/UI
  app:
    path: /*
    middlewares: [session, app]
```

With this wiring the [`login`](./login) page authenticates users against the auth middleware (password grant and/or passkey), the session middleware stores tokens server-side and validates them with the auth signing key, and the auth management API/UI receives the authenticated `X-User` header.

Set the `admin` runtime namespace to split normal self-service users from operators:

```json
{
  "permission": "turna.auth.admin",
  "allow_missing_x_user": true
}
```

When `permission` is set, requests with `X-User` must have that permission (by ID or name) to call management APIs or see admin UI tabs. Non-admin users still reach self-service (`#account`, `#device`). If `X-User` is missing and `allow_missing_x_user` is true, auth treats the request as break-glass admin; this is intended for local recovery after removing the session chain, not for public exposure. Empty `permission` keeps bootstrap-open behavior until you create the first admin permission/role.

### Remote deployment

The auth middleware can also run on a separate turna instance. Since all issuer endpoints are plain HTTP, session/login on other instances connect like to any OAuth2 IdP — drop `auth_middleware` and use URLs instead:

```yaml
provider:
  turna:
    password_flow: true
    passkey: true
    oauth2:
      client_id: "ui"
      scopes: [openid]
      cert_url: https://auth.example.com/auth/oauth2/certs
      token_url: https://auth.example.com/auth/oauth2/token
      passkey_url: https://auth.example.com/auth/oauth2/passkey
```

Remote notes:

- Keep `/auth/oauth2/*` publicly routable on the auth instance (no `session` in front); normally protect `/auth/v1/*` and `/auth/ui/*` with a session chain. If you intentionally remove the session chain for recovery, `admin.allow_missing_x_user=true` grants break-glass admin access to requests without `X-User`.
- Set the `passkey` runtime settings (`rp_id`, `origins`) when login pages are served from other domains; with the default derive-from-request behavior the login middleware forwards `X-Forwarded-Host`/`X-Forwarded-Proto`, so same-domain setups work without configuration.
- The session middleware fetches JWKS from `cert_url` at startup, so the auth instance must be reachable when dependent instances boot.

## Migration from iam/oauth2

- `iam` and `oauth2` middlewares are deprecated but still available for Badger-backed deployments.
- API payload shapes match the old IAM API (`data.Response`, `UserExtended`, etc.), so existing clients largely work after switching base paths to `/auth/v1`.
- Not ported: Badger binary backup/restore endpoints (`/v1/backup`, `/v1/restore`) and the Badger write-api/Redis sync model; PostgreSQL with version polling replaces them.
