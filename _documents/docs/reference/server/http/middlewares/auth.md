# auth

`auth` is the PostgreSQL-backed authentication middleware. It replaces the legacy [`iam`](./iam) + [`oauth2`](./oauth2) stack with one middleware that serves IAM, OAuth2, LDAP sync, and an embedded management UI.

The middleware behaves like a standalone app with its own UI: every runtime setting (OAuth2 redirect behavior, permission check rules, cache polling, token lifetimes, OAuth clients/providers, LDAP) lives in PostgreSQL and is managed through the API or UI. The static configuration only covers how to reach that database: encryption key, database connection, and migration settings.

Reads are served from an in-memory read model; writes go to PostgreSQL inside a transaction that bumps a version, records an event, and emits `pg_notify('auth_changed', version)`. Every instance keeps a dedicated `LISTEN auth_changed` connection and normally reloads immediately; version polling remains the durable fallback for disconnects or missed notifications.

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
| `database.dsn` | PostgreSQL connection string. Required. Each process also opens one dedicated connection for `LISTEN auth_changed`; when using PgBouncer this DSN must provide session semantics (session pooling or direct PostgreSQL) for immediate notifications. |
| `database.max_open_conns` | `database/sql` max open connection limit. Defaults to `5`; negative for unlimited. The dedicated notification connection is separate from this pool. |
| `database.max_idle_conns` | `database/sql` max idle connection limit. Defaults to `3`; negative for none. |
| `database.conn_max_lifetime` | Connection max lifetime. Defaults to `15m`; negative for unlimited. |
| `database.conn_max_idle_time` | Optional connection max idle time. |
| `database.migration.dsn` | Optional DSN used only while running migrations (e.g. a user with DDL privileges). Defaults to `database.dsn`; the connection is closed after migration. |
| `database.migration.disabled` | Disable built-in migration bootstrap. |
| `database.migration.values` | Optional `muz.Migrate` template substitution values. |
| `database.migration.table` | Migration tracking table. Defaults to `auth_migrations`. |
| `database.migration.lock_key` | PostgreSQL advisory lock key. Defaults to `muz:postgres:turna:auth_migrations`. |
| `encryption.key` | Required encryption key for secrets stored in PostgreSQL. Raw strings are SHA-256 derived; base64 16/24/32-byte keys are used directly. |
| `ldap.disable_sync` | Keep this instance out of the periodic LDAP sync loop; the manual sync API keeps working. Config-file only. Instances that do participate coordinate through the `auth_sync_locks` table, so a fleet sharing one database syncs once per `sync_duration` instead of once per instance. |

## Runtime settings (stored in PostgreSQL)

Everything else is a settings namespace under `/auth/v1/settings/{namespace}` and takes effect without a restart. The UI exposes dedicated pages for OAuth2, access checks, API keys, email and mTLS; the *Runtime Settings* page keeps the remaining operational namespaces.

| Namespace | Keys | Description |
| --- | --- | --- |
| `admin` | `permission`, `allow_missing_x_user` | Management API/UI authorization. Empty `permission` keeps bootstrap-open behavior. When set, `X-User` must have that permission ID/name. `allow_missing_x_user` defaults to `true` for break-glass access when the session chain is removed and no `X-User` is present. Do not expose this route publicly when break-glass is enabled. |
| `oauth2` | `base_url`, `schema`, `insecure_skip_verify` | Code-flow redirect behavior for upstream providers and the canonical token issuer. `schema` defaults to `https`. Set `base_url` when one auth instance serves session/login flows through multiple hosts, so a token issued through one host can be refreshed through another without an issuer mismatch. |
| `check` | `default_hosts`, `no_host_check` | Host rules for permission evaluation. |
| `cache` | `poll_interval`, `code_store` | Fallback version poll interval for the in-memory read model and OAuth2 temporary code/state store. PostgreSQL notifications normally propagate changes immediately; the poll catches changes missed during listener disconnects. `code_store.active` is `database` (default), `memory`, or `redis`. Database and Redis are shared across replicas; memory is single-instance only. |
| `token` | `token_lifetime`, `refresh_lifetime`, `refresh_absolute_lifetime` | Access lifetime, refresh idle window and remembered-session ceiling (defaults `15m` / `24h` / `720h`). |
| `jwt` | `kid`, `private_key` | RS256 signing key (PEM, PKCS#8 or PKCS#1); auto-generated on first start. Editable through the API/UI and applied without restart — the public JWKS key is derived from the private key. Changing or rotating the key invalidates outstanding tokens. |
| `passkey` | `disabled`, `rp_id`, `rp_display_name`, `origins`, `user_verification` | WebAuthn (passkey) relying party settings. Empty `rp_id` defaults to the registrable domain (eTLD+1) of the request host — e.g. `auth.example.com` becomes `example.com`, so one passkey works across all subdomains; IPs and single-label hosts (`localhost`) are used as-is. Empty `origins` derives from the forwarded scheme + host. |
| `password` | `disabled`, `local_disabled`, `ldap_disabled`, `ldap_register_disabled` | Password grant sources. Defaults keep the implicit behavior: local users check bcrypt, non-local users bind against LDAP, and unknown aliases are created only after a successful LDAP bind. |
| `api_key` | `disabled`, `self_service`, `max_lifetime` | Static API key creation and validation. `self_service` (default off) lets any authenticated X-User issue and manage their own keys through `/v1/api-keys` — a "Personal access keys" panel appears on the account page. `max_lifetime` caps the expiry of new keys (duration string); empty means keys may live forever. |
| `device` | `disabled`, `code_lifetime`, `interval`, `verification_uri` | RFC 8628 device flow. Defaults: codes live `10m`, minimum poll interval `5` seconds, verification URI `<prefix>/ui/device`. |
| `token_exchange` | `disabled` | RFC 8693 token exchange grant. |
| `totp` | `disabled`, `issuer`, `skew` | TOTP second factor. `issuer` is shown in authenticator apps (default `Turna Auth`), `skew` is the allowed period drift (default `1` = ±30s). Confirming TOTP also issues 8 single-use recovery codes. |
| `email` | `disabled`, `magic_link`, `from`, `subject`, `body_template`, `magic_link_subject`, `magic_link_body_template`, `code_lifetime`, `smtp.{host,port,username,password,no_auth,starttls,tls,insecure_skip_verify}` | Passwordless email login with two independent mails: the one-time code (`disabled`, `subject`, `body_template`) and the magic link (`magic_link` default true, `magic_link_subject`, `magic_link_body_template`). All templates are Go `text/template` strings; empty uses built-in defaults. Set `smtp.no_auth=true` for trusted relays that need no authentication. Codes live `15m` by default. The relay is also used by `signup`. Login is effectively off until `smtp.host` is set. |
| `signup` | `enabled`, `email_verification`, `password_reset`, `default_role_ids`, `code_lifetime`, `verify_subject`, `verify_body_template`, `reset_subject`, `reset_body_template` | Self-registration and forgot-password flows (UI: *Signup*). Off by default. `email_verification` defaults to `true`; verification/reset mails use the `email` SMTP relay. Codes live `1h` by default. Templates are Go `text/template` strings validated on save. |
| `mtls` | `enabled`, `cert_header`, `cert_verify_header`, `cert_verify_value`, `trusted_proxy_cidrs` | Certificate based client authentication (RFC 8705 style). Native mode requires listener `client_ca_files`. Proxy mode requires certificate and verification-result headers plus an immediate-peer allowlist. Off by default. |
| `saml` | `certificate`, `private_key` | SAML SP signing key pair; auto-generated (self-signed, 10 years) on first SAML use. |
| `authorize` | `disabled`, `flow_lifetime`, `login_url` | Local browser-based authorization code flow (`/oauth2/authorize` + consent screen). Pending consents live `10m` by default. `login_url` redirects anonymous browsers to a login page (with `?redirect_path=` back reference, the [`login`](./login) middleware convention); empty shows an error asking to log in first. |
| `registration` | `enabled`, `client_lifetime`, `default_scope`, `max_clients` | RFC 7591 dynamic client registration (`/oauth2/register`; UI: *OAuth2 → Dynamic client registration*). Off by default because registration is anonymous. `client_lifetime` expires dynamic clients (empty keeps them forever), `max_clients` caps stored dynamic clients (default `1000`). |
| `custom_info` | `disabled`, `sets` | Per-name userinfo claim templates served at `/oauth2/userinfo/{custom}` (and advertised by `/oauth2/openid/{custom}/.well-known/openid-configuration`). `sets.<name>.claims` maps an output claim to a Go `text/template`; templates receive <code v-pre>{{ .claims }}</code> (the base userinfo claims) and <code v-pre>{{ .user }}</code> (the full user record). A template whose key is new adds a claim, an existing key overwrites it, and a template that renders empty (e.g. `""` or trimmed with <code v-pre>{{- -}}</code>) removes the claim. Templates are validated on save. Managed in the UI under *Custom Info*. |
| `session_providers` | `providers`, `groups` | A [`session`](./session) middleware provider list managed from the UI (*Federation → Session providers*) instead of static YAML. `providers` is a map keyed by provider name using the session middleware's provider model (`name`, `auth_middleware`, `oauth2.*`, `passkey`, `password_flow`, `api_key`, `x_user`, `claim_header`, `priority`, `hide`, ...); `groups.<name>.providers` holds independent definitions and `groups.<name>.inherit` references canonical top-level providers without copying credentials. An inherited entry may set `hide` to override login visibility for that group. Full provider definitions remain globally unique, while one top-level provider may be inherited by any number of groups. Groups can be pulled selectively with `provider_source.group` or `GET /v1/session-providers/{group}`. A session middleware pulls the list with `provider_source.auth_middleware` (in-process, applied on the next request after a commit) or over `GET /v1/session-providers` with `provider_source.url` (remote, TTL polled). Dynamic providers override same-named static ones. |

Example:

```sh
curl -X PUT /auth/v1/settings/oauth2 -d '{"value":{"schema":"https","base_url":""}}'
curl -X PUT /auth/v1/settings/check -d '{"value":{"no_host_check":true}}'
curl -X PUT /auth/v1/settings/cache -d '{"value":{"poll_interval":"5s","code_store":{"active":"redis","redis":{"address":["redis:6379"]}}}}'
```

## Storage

Migrations are embedded and run through `github.com/rakunlabs/muz` with a PostgreSQL advisory lock. The schema includes:

- `auth_versions`, `auth_events` — monotonic change version and durable event log.
- `auth_settings` — encrypted JSON settings namespaces. Reserved: `admin`, `jwt`, `token`, `oauth2`, `check`, `cache`, `api_key`, `device`, `token_exchange`, `totp`, `email`, `signup`, `mtls`, `saml`, `custom_info`, `authorize`, `registration`, `session_providers` (see [Runtime settings](#runtime-settings-stored-in-postgresql)).
- `auth_oauth_clients`, `auth_oauth_providers`, `auth_ldap_configs`, `auth_saml_providers` — encrypted config records.
- `auth_users` — IAM users; the `details` map is encrypted at rest, passwords are bcrypt hashed.
- `auth_roles`, `auth_permissions`, `auth_lmaps` — IAM model.
- `auth_api_keys` — api keys (sha256 hashes; the key itself is never stored).
- `auth_totp_secrets` — encrypted TOTP shared secrets.
- `auth_flow_codes` — short-lived flow state shared between instances (OAuth codes/state, passkey challenges, device and email codes, SAML relay states, pending consents, revoked token ids).

## Routes

With `prefix_path: /auth`:

### Management UI

| Route | Purpose |
| --- | --- |
| `/auth/ui/*` | Embedded Svelte management UI (users, roles, permissions, LDAP, OAuth, settings, API keys, email, mTLS, self-service account). |
| `/auth/ui/#account` | Self-service account page for the current `X-User`: password, TOTP recovery, passkeys, roles and permissions. With `api_key.self_service` on it also carries a "Personal access keys" panel for issuing and revoking own API keys. |
| `/auth/ui/#api-keys` | Admin API key principal management: choose an owner user/service account, create with a lifetime, attach role/permission IDs, copy the one-time key, list/update/revoke existing keys, and edit API key runtime settings. |
| `/auth/ui/#email` | Email code login settings: SMTP relay, code mail Go-template subject/body editor and preview. |
| `/auth/ui/#magic-link` | Magic link login settings: enable toggle, magic link mail template editor and preview (shares the `email` SMTP relay). |
| `/auth/ui/#signup` | Self-registration settings: signup/verification/password-reset toggles, default roles, code lifetime, mail template editors with preview. |
| `/auth/ui/#oauth2-overview` | OAuth2 token, authorization, dynamic client registration, password/passkey, and signing-key settings. |
| `/auth/ui/device?user_code=XXXX-XXXX` | RFC 8628 device approval/deny page; `user_code` is optional and pre-fills the form when present. |
| `/auth/ui/#mtls` | Global mTLS settings and workflow guide; certificate bindings live on service account records. |
| `/auth/ui/#custom-info` | Custom userinfo template sets: per-name claim template editor (add/overwrite/remove claims) with a live preview against sample claims and user details. |
| `/auth/ui/#session-providers` | Session middleware provider list (`session_providers` namespace): per-provider editor (auth middleware binding, OAuth2 endpoints, passkey/password/API key toggles, claim headers, optional group assignment) with wiring examples for `provider_source`. |
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
| `POST` | `/auth/v1/check` | Permission check by optional alias/id + host/path/method. Without identity, checks public permissions only. |
| `POST` | `/auth/check` | Permission check for the `X-User` header identity. Without `X-User`, a public match returns allowed; a private request returns 401. |
| `GET` | `/auth/info` | Identity info for the `X-User` header. |
| `GET` | `/auth/v1/dashboard` | Totals and extended roles. |

List endpoints parse the query string with [`rakunlabs/query`](https://pkg.go.dev/github.com/rakunlabs/query): use `_limit`/`_offset` for paging (legacy `limit`/`offset` keys still work) plus field filters such as `name=...`, `role_ids=...`, `add_roles=true`.

#### Public permission resources

A permission can be marked `public: true` in the permission editor or API. Its resource list then applies to everyone, including requests without `X-User`. It uses the ordinary permission matcher: `hosts`, doublestar `paths`, case-insensitive `methods`, and `excluded` resources all continue to apply.

```json
{
  "name": "public-pages",
  "public": true,
  "resources": [
    {
      "hosts": ["app.example.com"],
      "paths": ["/health", "/docs/**"],
      "methods": ["GET"],
      "excluded": [
        {"paths": ["/docs/internal/**"], "methods": ["GET"]}
      ]
    }
  ]
}
```

A [`session`](./session) middleware in front picks these up when this auth is listed in its `auth_skip_paths`: anonymous requests matching a public permission pass through session with identity headers stripped instead of being redirected to login (remote deployments list the check endpoint URL instead of the name). Alternatively open the route by hand with session `skip_paths`. Behind session, `iam_check` performs the same check — directly with `auth_middleware: <name>` (recommended) or via the remote check API — and passes the request only when a public permission matches. Authenticated users also get public access without having the permission assigned through a role.

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
| `GET` | `/auth/oauth2/authorize` | Local authorization endpoint (code flow with consent screen); see [Local authorize + consent](#local-authorize--consent). |
| `GET/POST` | `/auth/oauth2/consent` | Consent page for a pending authorize flow (browser plane, needs `X-User` from the session middleware). |
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
| `GET` | `/auth/oauth2/userinfo` | Userinfo for a bearer access token. Mirrors the roles the presented token carries, at that token's `roles_claim` path. |
| `GET` | `/auth/oauth2/userinfo/{custom}` | Userinfo with the named `custom_info` claim templates applied (add or overwrite claims). |
| `POST` | `/auth/oauth2/revoke` | RFC 7009 token revocation. Access tokens are denied by `jti`; refresh tokens revoke their complete `sid` session family. |
| `POST` | `/auth/oauth2/introspect` | RFC 7662 token introspection (client-authenticated); returns `active` plus token claims. |
| `POST` | `/auth/oauth2/register` | RFC 7591 dynamic client registration (requires the `registration` setting). |
| `GET/PUT/DELETE` | `/auth/oauth2/register/{client_id}` | RFC 7592 client management with the `registration_access_token` returned at registration. |
| `GET` | `/auth/oauth2/.well-known/openid-configuration` | OpenID configuration built from the request host. |
| `GET` | `/auth/oauth2/.well-known/oauth-authorization-server` | RFC 8414 authorization server metadata (same document). Root path-insertion variants `/.well-known/oauth-authorization-server[/...]` and `/.well-known/openid-configuration[/...]` are also registered and take effect when the router forwards `/.well-known/*` to this middleware. |
| `GET` | `/auth/oauth2/openid/{custom}/.well-known/openid-configuration` | Same OpenID configuration, but `userinfo_endpoint` points to `/auth/oauth2/userinfo/{custom}` so discovery-driven clients pick up that set's `custom_info` claims. `issuer` and the other endpoints stay shared (the `issuer` matches the `id_token` `iss`). |

Token notes:

- `client_credentials` authenticates service accounts via the `secret` detail. With the `mtls` setting enabled and no secret provided, a client certificate verified during the native TLS handshake or by an explicitly trusted TLS-terminating proxy is matched against the service account's `cert_fingerprint` (sha256 of the DER cert) or `cert_subject` detail.
- `password` checks local users with bcrypt, or LDAP when the user is not `local`. Unknown LDAP users are created on first successful sync.
- The `password` settings namespace makes those sources explicit and switchable: `disabled` rejects the grant entirely, `local_disabled`/`ldap_disabled` block one source, `ldap_register_disabled` stops auto-creating unknown users from LDAP. An unknown alias must pass its LDAP bind before any directory sync; first-login sync queries only that user's group memberships rather than loading the complete group tree. Managed in the UI under *OAuth2 → Password Login*.
- Users with a confirmed TOTP secret must send a `totp` form field on the password grant; a missing code answers `401` with `error=mfa_required`. A single-use recovery code is accepted in place of the TOTP code.
- OAuth clients come from `/auth/v1/oauth/clients`; service accounts work as a confidential-client fallback. Empty secrets are accepted only for OAuth client records explicitly marked `public` (including dynamic `token_endpoint_auth_method=none` clients), and only on flows that support public clients. A service account without a secret must authenticate `client_credentials` with mTLS.
- Token lifetimes come from the `token` settings namespace (default access `15m`, refresh idle `24h`, remembered-session maximum `720h` / 30 days).
- Initial grants accept the `remember_me` extension parameter. Without it, the original refresh token remains reusable until its fixed `refresh_lifetime` expiry. With it, refreshes mint a new refresh token with a sliding `refresh_lifetime` window, capped by the family's immutable `refresh_absolute_lifetime` boundary. Refresh requests cannot change this choice; the signed token claim is authoritative.
- Access, ID and refresh tokens carry a shared `sid`, `auth_time` and `session_exp`. This keeps parallel browser refreshes in one revocable family and gives every family a fixed absolute boundary.
- Once the disabled state reaches an instance's cache, token issuance rejects that user or service account as either the token subject or OAuth client, including password, authorization-code, refresh, device, email, passkey, token-exchange, client-secret and mTLS paths. Existing self-contained access tokens may remain cryptographically valid until expiry for external JWT-only consumers; in-process cache checks reject the disabled principal as soon as they observe the update.
- An `id_token` is issued whenever the granted scope contains `openid`; the `nonce` from the authorization request is embedded for code-flow logins.
- **Roles claim:** a user's roles are derived from the *granted scopes* — a permission's `scope` map (`{"<scope>": ["<role>"]}`) contributes its roles when the token request asks for that scope (or the client lists it in its default `scope`). The resulting list is written to the access token, the `id_token` and the `/oauth2/userinfo` response, all at the same dot path: the client record's `roles_claim` when set, otherwise the `token` namespace's `roles_claim` (default: the flat `roles` claim). A permission with an empty `scope` map grants access through `/v1/check` but puts nothing in a token.
- **PKCE (RFC 7636):** `/auth/oauth2/auth/{provider}` and `/auth/saml/{provider}/login` accept `code_challenge` (+ `code_challenge_method`, `S256` or `plain`); the `authorization_code` grant then requires a matching `code_verifier`. Public clients (no stored secret) must use PKCE.
- **Redirect whitelist:** federated OAuth and SAML login requests require `client_id` and a non-empty `redirect_uri`. When `whitelist_urls` is set, it is validated by prefix match. Clients with registered `redirect_uris` (dynamic registration or manual config) use exact matching instead. If neither list is configured, every non-empty redirect target is accepted.
- **Code binding:** every authorization code is bound to the requesting `client_id` and `redirect_uri`; the token endpoint rejects missing bindings, redemption by another client, and a missing or different redirect target. Components that mint codes into the same store — the [`login`](./login) middleware's `response_type=code` flow among them — must therefore pass `client_id` and `redirect_uri` through.
- **Client authentication encoding:** RFC 6749 §2.3.1 wants the `client_id`/`client_secret` of a `Authorization: Basic` header form-urlencoded, which is what Go's `oauth2` package and this project's own client do, so an id or secret holding `@`, `+`, `/` or `=` arrives percent-encoded. Both forms are accepted: the verbatim value is tried first, the decoded one as a fallback. Credentials sent in the request body are used as-is.
- **Resource indicators (RFC 8707):** `resource` parameters on `/oauth2/authorize`, `/oauth2/auth/{provider}` and the token endpoint land in the access token `aud` claim (next to `turna-auth`), so resource servers can require their own identifier. A client record may pin allowed resources with `resources` (prefix match; empty allows any).

- **Client ID Metadata Documents:** a `client_id` that is an HTTPS URL with a path (e.g. Claude Code's `https://claude.ai/oauth/claude-code-client-metadata`) needs no registration — the document is fetched live (public addresses only, no redirects, 5 KB cap, JSON) and describes a public PKCE client with exact-match `redirect_uris`. To pin policy for such a client, save a client record **under the URL as its id** (the client API accepts slashes in ids): its `resources`, `scope`, `skip_consent` and `roles_claim` overlay the live document, while identity and redirect targets stay authoritative in the fetched metadata. If the metadata host is unreachable, a stored record acts as a full fallback (then its own `redirect_uris`/`whitelist_urls` apply).
- **Revocation:** access tokens use their `jti`; revoking a refresh token stores one `revoked_session` record for its `sid`, invalidating old and new family members together. Expired flow/revoke rows are purged at startup, hourly, or manually with `POST /auth/v1/maintenance/flow-codes/purge`.

### Local authorize + consent

`GET /auth/oauth2/authorize` implements the standard browser-based authorization code flow against local users (enabled by default; the `authorize` namespace can disable or tune it):

1. The client opens `/auth/oauth2/authorize?response_type=code&client_id=...&redirect_uri=...&scope=...&state=...&code_challenge=...&code_challenge_method=S256[&resource=...]`. Public clients (no stored secret) must send PKCE.
2. The request is validated (client, redirect target, PKCE, resources), stored as a pending flow and the browser is redirected to `/auth/oauth2/consent?flow=...`.
3. The consent page needs a logged-in user: it reads `X-User` set by the [`session`](./session) middleware in front. List this middleware in session `auth_skip_paths: ["<name>"]` so the public plane (`/auth/oauth2/**`, `/auth/saml/**`, root discovery documents) is skip-pathed; alternatively use session `skip_paths: ["/auth/oauth2/**"]`. Machine endpoints stay public while cookie-carrying browsers are still authenticated on the consent page. Anonymous browsers are redirected to `authorize.login_url` (with `?redirect_path=`) when configured. Clients with `skip_consent: true` are auto-approved; everyone else sees an approve/deny screen with client name and scopes.
4. Approval issues a single-use code bound to client + redirect + PKCE + resources and redirects back to `redirect_uri?code=...&state=...`; the client exchanges it at the token endpoint with `code_verifier`.

### MCP / resource server integration

The combination of RFC 8414 metadata, dynamic client registration, PKCE, resource indicators and the consent flow makes the auth middleware a drop-in authorization server for MCP clients (Claude, Cursor, ...):

1. Enable registration: `PUT /auth/v1/settings/registration {"value":{"enabled":true,"client_lifetime":"720h"}}`.
2. Route `/.well-known/*` to the auth middleware so RFC 8414 discovery works from the issuer root.
3. Keep the machine endpoints public: list this middleware in session `auth_skip_paths: ["<name>"]` (the consent page still authenticates cookie-carrying browsers, and `authorize.login_url` catches anonymous ones). Otherwise add `skip_paths: ["/auth/oauth2/**", "/.well-known/**"]` to the [`session`](./session) middleware in front, or split the router so `/auth/oauth2/*` bypasses session.
4. Protect the MCP endpoint with the [`oauth2_resource`](./oauth2_resource) middleware, which serves the RFC 9728 protected resource metadata and validates bearer tokens in-process.

An MCP client then discovers the resource metadata, registers itself (`/oauth2/register`), sends the user through `/oauth2/authorize` + consent, and calls the MCP endpoint with the issued bearer token — no manual client setup per user.

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

Together with the other X-User plane routes (`/v1/passkey/*`, `/v1/totp*`, `/v1/device`), this gives users self-service over interactive credentials. Recovery codes: `POST /auth/v1/totp/recovery` regenerates the set (old codes become invalid). API keys are managed from the admin *System -> API Keys* page by default; turning on the `api_key.self_service` setting additionally gives every signed-in user a "Personal access keys" panel on the account page (backed by the owner-scoped `/v1/api-keys` routes).

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
| `GET/POST/PATCH/DELETE` | `/auth/v1/api-keys...` | X-User owner-scoped plane (personal access keys). Admin-only by default; with the `api_key.self_service` setting on, any authenticated X-User can issue, list, update and revoke **their own** keys here. A key never carries more than its owner's roles/permissions. The admin management UI uses `api-key-principals`. |

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
| `GET` | `/auth/saml/{provider}/login` | Start a login; `client_id` and `redirect_uri` required, `state`/`scope` optional. Public clients must also send PKCE parameters. |
| `POST` | `/auth/saml/{provider}/acs` | Assertion consumer service (IdP POST binding callback). |

Provider config keys: `metadata_url` or inline `metadata_xml`, optional `entity_id`, `alias_attribute` (default: email-like attributes, then the subject NameID) and `sign_requests` (RSA-SHA256 with the auto-generated SP key from the `saml` settings namespace). After the assertion is validated the user is redirected to `redirect_uri?code=...&state=...` and the same client exchanges the code with the standard `authorization_code` grant, including the identical `redirect_uri` — same shape as the upstream OAuth2 provider flow.

### mTLS client credentials

mTLS uses service accounts as clients:

1. Enable the `mtls` setting namespace (`enabled: true`) and choose one trust mode below.
2. Create or edit a service account. Its first alias is the OAuth2 `client_id`.
3. Fill `details.cert_fingerprint` (sha256 DER hex, recommended) or `details.cert_subject` on that service account. The UI can calculate the fingerprint from a pasted PEM certificate.
4. Request `/auth/oauth2/token` with `grant_type=client_credentials` and `client_id=<alias>` while presenting the certificate. A client secret is not required for mTLS-only clients.

**Native TLS mode:** configure one or more client CA bundles on the Turna listener and leave `mtls.cert_header` empty:

```yaml
server:
  http:
    tls:
      client_ca_files:
        - ./client-ca.pem
    # routers on this entrypoint still use tls: {}
```

The listener uses `VerifyClientCertIfGiven`: ordinary HTTPS requests need no client certificate, but any certificate a client presents must chain to one of these CAs. The auth middleware accepts only `r.TLS.VerifiedChains`; an unverified `PeerCertificates` entry is never treated as identity. Certificate validity and client-auth EKU are enforced.

**TLS-terminating proxy mode:** the immediate proxy must verify the client chain and private-key proof, strip incoming identity headers, and write both the certificate and verification result itself. Configure all parts of the trust boundary:

```json
{
  "enabled": true,
  "cert_header": "ssl-client-cert",
  "cert_verify_header": "ssl-client-verify",
  "cert_verify_value": "SUCCESS",
  "trusted_proxy_cidrs": ["192.0.2.10", "2001:db8:10::5"]
}
```

For nginx, the corresponding values are normally `$ssl_client_escaped_cert` and `$ssl_client_verify`. Turna checks the connection's direct `RemoteAddr` against `trusted_proxy_cidrs`; it never trusts `X-Forwarded-For` for this decision. Prefer exact proxy addresses or a narrowly dedicated subnet, and enforce the same boundary with firewall/network policy because every trusted peer can assert certificate identity. A certificate header without an allowed peer and successful verification header is rejected. Existing configurations that set only `cert_header` must add these trust fields before header mode works again.

Session integration is token based: mTLS authenticates the token request, not the session middleware directly. Use the issued access token as a bearer token on routes protected by `session`.

### Encrypted config

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/auth/v1/info` | Prefix, storage type, and current auth version. |
| `GET` | `/auth/v1/capabilities` | Current request capabilities (`is_admin`, `anonymous_admin`, configured admin permission). The UI uses this to hide admin pages from normal users. |
| `GET/PUT/DELETE` | `/auth/v1/settings`, `/auth/v1/settings/{namespace}` | Encrypted JSON settings. Writes apply immediately on the handling instance; the `jwt` namespace is validated (parseable private key + kid) before saving. |
| `GET` | `/auth/v1/session-providers` | The UI-managed session middleware provider list (`session_providers` namespace, ungrouped providers plus every group merged) with the auth version in `meta.version`. Remote turna instances poll it through the session middleware's `provider_source.url`; admin-protected because provider client secrets travel in the payload. |
| `GET` | `/auth/v1/session-providers/{group}` | One named group of the same list, so different session middleware instances can pull different subsets. Same envelope and admin protection; `404` when the group is unknown. |
| `POST` | `/auth/v1/custom-info/preview` | Render an unsaved `custom_info` set against sample claims and user details; returns the resulting claims and validates the templates (used by the UI). |
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

Passkey support uses the dependency-free engine from `github.com/rakunlabs/ada/middleware/auth/strategy/passkey`. Credentials are stored in `auth_passkey_credentials`; in-flight challenges use the OAuth2 code store (`cache.code_store`). The default database store and Redis work across instances; memory does not.

| Method | Route | Purpose |
| --- | --- | --- |
| `POST` | `/auth/v1/passkey/register` | Begin/finish registration (begin without `credential`, finish with `session_id` + `credential`). Without `user_id` targets the `X-User` identity (self-service); with `user_id` registers for that user and requires admin capability. |
| `GET` | `/auth/v1/passkey/credentials` | List passkeys for the `X-User` identity. Listing another `user_id` requires admin capability. |
| `DELETE` | `/auth/v1/passkey/credentials/{id}` | Delete a stored passkey. |
| `POST` | `/auth/oauth2/passkey` | Public login ceremony; finish responds with the standard token JSON. |

Login requests carry `client_id`/`client_secret` like the password grant; `username` scopes `allowCredentials` to a known user, empty uses the discoverable (passwordless) flow. `allowCredentials` and `excludeCredentials` include the transports captured at registration (`internal`, `hybrid`, `usb`, ...) so browsers surface the matching authenticator — a synced browser-profile passkey shows up directly instead of the generic QR/security-key dialog. The management UI shows registered passkeys on the user page; admins can enroll a passkey for the selected user, which binds the authenticator present in the operator's browser to that user's account. The self-service Account page enrolls for the signed-in user.

#### Registering and listing passkeys from an external UI

The `/v1/passkey/*` routes are part of the X-User plane: they carry no client credentials and identify the caller by the `X-User` header injected by a fronting [`session`](./session) middleware. An external (custom) account page therefore calls them same-origin from the signed-in browser — the session cookie authenticates the request, and without a resolved `X-User` the endpoints answer `401`. Passing a `user_id` other than the caller's own requires admin capability; when the routes are additionally guarded by IAM permission checks, grant the self-service resource `{prefix}/v1/passkey/**` with methods `GET`, `POST`, `DELETE`. All endpoints answer `503 passkey not available` while the `passkey` runtime setting has `disabled: true`.

**Register** is a begin/finish pair on `POST /auth/v1/passkey/register`. Begin with an empty object for the signed-in user (or `{"user_id": "..."}` as admin):

```http
POST /auth/v1/passkey/register
Content-Type: application/json

{}
```

```json
{
  "payload": {
    "session_id": "01J8ZQ...",
    "options": {
      "challenge": "1nZFbXVK...",
      "rp": {"id": "example.com", "name": "Turna Auth"},
      "user": {"id": "dXNlcklk", "name": "alice", "displayName": "Alice"},
      "pubKeyCredParams": [{"type": "public-key", "alg": -7}],
      "timeout": 60000,
      "excludeCredentials": [
        {"type": "public-key", "id": "kFum-Ci0...", "transports": ["internal"]}
      ],
      "authenticatorSelection": {"residentKey": "preferred", "userVerification": "preferred"},
      "attestation": "none"
    }
  }
}
```

All binary fields are URL-safe base64 without padding. Decode `challenge`, `user.id` and each `excludeCredentials[].id` into buffers, run `navigator.credentials.create({publicKey})`, then base64url-encode the result and finish with the same `session_id` plus a user-facing `name` label:

```http
POST /auth/v1/passkey/register
Content-Type: application/json

{
  "session_id": "01J8ZQ...",
  "name": "MacBook Touch ID",
  "credential": {
    "id": "kFum-Ci0...",
    "rawId": "kFum-Ci0...",
    "type": "public-key",
    "response": {
      "clientDataJSON": "eyJ0eXBl...",
      "attestationObject": "o2NmbXQ...",
      "transports": ["internal", "hybrid"]
    },
    "authenticatorAttachment": "platform"
  }
}
```

Success returns `{"payload": {"message": "passkey registered", "id": "<base64url credential id>"}}`. The begin session is single use with a 2-minute TTL — a failed or expired finish needs a new begin. Include `"user_id"` in **both** requests when enrolling for another user as admin. Send `response.transports` (from `credential.response.getTransports()`) when available: it is stored and later replayed in `allowCredentials`, giving users the direct platform/hybrid prompt at login.

**List** the stored passkeys with `GET /auth/v1/passkey/credentials` (own) or `?user_id=...` (admin):

```json
{
  "meta": {"total_item_count": 2},
  "payload": [
    {
      "id": "kFum-Ci0...",
      "user_id": "01J8ZP...",
      "name": "MacBook Touch ID",
      "sign_count": 4,
      "created_at": "2026-08-01T09:30:00Z",
      "updated_at": "2026-08-20T14:05:00Z"
    }
  ]
}
```

`id` is the base64url credential ID and doubles as the delete path parameter: `DELETE /auth/v1/passkey/credentials/{id}` (URL-encode it) removes the passkey and answers `{"payload": {"message": "passkey deleted"}}`, `404` when unknown. Users can delete their own credentials; anything else requires admin. The embedded UI's `AccountTab.svelte` and `PasskeyPanel.svelte` (`pkg/server/http/middleware/auth/_ui/src/components/`) are the reference implementations, including the base64url conversions in `_ui/src/lib/webauthn.ts`.

## Session/login integration

The middleware registers itself as an in-process token issuer under its middleware name. A [`session`](./session) provider can reference it with `auth_middleware` so JWT validation (key lookup) and refresh happen in-process — no `cert_url`/`token_url` self-calls:

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
- Set the `passkey` runtime settings (`rp_id`, `origins`) when login pages are served from an unrelated domain; the default derives `rp_id` as the registrable domain (eTLD+1) of the forwarded host, so auth and login pages on sibling subdomains (`auth.example.com`, `app.example.com`) already share one passkey scope without configuration.
- The session middleware fetches JWKS from `cert_url` at startup, so the auth instance must be reachable when dependent instances boot.

## Migration from iam/oauth2

- `iam` and `oauth2` middlewares are deprecated but still available for Badger-backed deployments.
- API payload shapes match the old IAM API (`data.Response`, `UserExtended`, etc.), so existing clients largely work after switching base paths to `/auth/v1`.
- Not ported: Badger binary backup/restore endpoints (`/v1/backup`, `/v1/restore`) and the Badger write-api/Redis sync model; PostgreSQL notifications with version polling fallback replace them.
