# session

`session` validates bearer tokens or access tokens stored in a server-side session. It also sets identity headers for later middlewares and upstream services.

```yaml
server:
  http:
    middlewares:
      session:
        session:
          cookie_name: auth_session
          session_key: my-secret-key
          store:
            active: file
            file:
              path: ./sessions
          options:
            path: /
            max_age: 86400
            http_only: true
            same_site: 2
          provider:
            keycloak:
              password_flow: true
              oauth2:
                client_id: app
                client_secret: ""
                scopes: [openid]
                cert_url: http://localhost:8080/realms/master/protocol/openid-connect/certs
                token_url: http://localhost:8080/realms/master/protocol/openid-connect/token
                auth_url: http://localhost:8080/realms/master/protocol/openid-connect/auth
          action:
            token:
              login_path: /login/
```

## Store

```yaml
session_key: my-secret-key
store:
  active: redis # redis or file
  redis:
    address: localhost:6379
    username: ""
    password: ""
    key_prefix: session_
    session_key: "" # optional store-specific override
    compat: ""      # "", v1 or mixed
    tls:
      enabled: false
      cert_file: ""
      key_file: ""
      ca_file: ""
  file:
    session_key: "" # optional store-specific override
    path: ""
```

If `active` is empty, Turna uses `redis` when configured, otherwise `file` when configured. A store is required. The top-level `session_key` is used by both Redis and file stores unless that store defines its own `session_key`.

## Redis Compatibility Mode

Turna v0.8.x stored the raw session ID in an unsigned cookie and the session values as gob. The current format signs the cookie and stores the values as JSON. The two are not interchangeable: if both versions serve the same cookie name, each one keeps invalidating the other's session and the browser ends up in a redirect loop.

`store.redis.compat` lets a newer Turna speak the older format so both versions can share one session.

| Mode | Reads | Writes | Use when |
| --- | --- | --- | --- |
| `""` (default) | JSON, signed cookie | JSON, signed cookie | Only the current version runs. |
| `v1` | gob, unsigned cookie | gob, unsigned cookie | A Turna v0.8.x shares the same cookie name and key prefix. |
| `mixed` | both | JSON, signed cookie | The older Turna is retired and sessions should migrate without a logout. |

Sharing a session requires `cookie_name` and `store.redis.key_prefix` to be identical in both versions.

```yaml
session:
  session:
    cookie_name: app_auth
    store:
      active: redis
      redis:
        address: localhost:6379
        key_prefix: app_auth_
        session_key: "a-32-byte-or-longer-random-value"
        compat: v1
```

Suggested rollout:

1. Deploy the new Turna with `compat: v1`. Both versions now share one session, so users log in once.
2. Retire the old Turna.
3. Switch to `compat: mixed`. Existing sessions move to the signed cookie as they are used, with no logout.
4. Remove `compat` once traffic has rolled over.

In `v1` the cookie is unsigned, so `session_key` has no effect and a client can choose its own session ID. This matches how v0.8.x already behaves, so it is not a regression, but it is a reason to keep the compatibility window short. Signing becomes effective in step 3.

## Cookie Options

| Field | Default | Description |
| --- | --- | --- |
| `cookie_name` | `auth_session` | Default session cookie name. |
| `cookie_name_hosts` | | Override cookie name by exact host or regex. |
| `options.path` | `/` | Cookie path. |
| `options.max_age` | `86400` | Cookie lifetime in seconds. |
| `options.domain` | | Cookie domain. |
| `options.secure` | `false` | Secure cookie flag. |
| `options.http_only` | `false` | HttpOnly cookie flag. |
| `options.same_site` | `0` | Go `http.SameSite`; `2` is Lax, `3` is Strict, `4` is None. |

## Provider

Provider entries describe OAuth2/OIDC token endpoints and claim handling.

```yaml
provider:
  keycloak:
    name: Keycloak
    x_user: [email, preferred_username, name]
    claim_header:
      X-User-Email: email
    email_verify_check: false
    password_flow: true
    priority: 0
    hide: false
    oauth2:
      client_id: app
      client_secret: ""
      scopes: [openid]
      cert_url: http://idp/certs
      introspect_url: ""
      userinfo_url: ""
      revocation_url: ""
      auth_url: http://idp/auth
      token_url: http://idp/token
      logout_url: http://idp/logout
```

`session` sets `X-User` from the first available claim in `x_user`, defaulting to `email`, `preferred_username`, then `name`. It also sets `X-User-Id` from `preferred_username` when present.

### In-process auth provider

A provider can be backed by an in-process [`auth`](./auth) middleware instead of remote URLs. Token validation uses the auth signing key directly and refresh runs in-process, so `cert_url`/`token_url` are not needed:

```yaml
provider:
  turna:
    auth_middleware: "auth"   # middleware key of the auth instance
    password_flow: true
    passkey: true             # advertise WebAuthn login on the login page
    api_key: true             # accept static X-API-Key credentials
    oauth2:
      client_id: "ui"         # OAuth client registered in auth
      scopes: [openid]
```

| Field | Description |
| --- | --- |
| `auth_middleware` | Name of the auth middleware instance to use as token issuer. |
| `passkey` | Show a passkey button on the login page for this provider. Requires `auth_middleware` (in-process) or `oauth2.passkey_url` (remote). |
| `api_key` | Accept static API keys on protected routes. The key is validated directly against the auth middleware database on every request; no token exchange. Downstream services receive the key principal's claims and `X-User: api-key:<id>`. |
| `api_key_header` | Header carrying the raw API key. Defaults to `X-API-Key`. |

### Remote auth provider

When the auth middleware runs in another turna instance, point the provider at it over HTTP like any other OAuth2 IdP — no `auth_middleware` needed:

```yaml
provider:
  turna:
    password_flow: true
    passkey: true
    api_key: true
    oauth2:
      client_id: "ui"
      scopes: [openid]
      cert_url: https://auth.example.com/auth/oauth2/certs
      token_url: https://auth.example.com/auth/oauth2/token
      passkey_url: https://auth.example.com/auth/oauth2/passkey
      api_key_url: https://auth.example.com/auth/oauth2/api-key
```

| Field | Description |
| --- | --- |
| `oauth2.passkey_url` | Remote auth middleware's WebAuthn begin/finish endpoint. The login middleware forwards the original host/scheme as `X-Forwarded-Host`/`X-Forwarded-Proto` so the relying party is derived from the login page, not the auth host. |
| `oauth2.api_key_url` | Remote auth middleware's static API key validation endpoint. Required for `api_key: true` on remote providers; in-process `auth_middleware` providers don't need it. |
| `oauth2.signup_url` | Remote auth middleware's self-registration endpoint (e.g. `https://auth.example.com/auth/oauth2/signup`); the verify endpoint is derived as `signup_url + "/verify"`. Lets the login page offer "Create account" for remote providers; in-process providers detect it automatically. |
| `oauth2.password_reset_url` | Remote auth middleware's forgot-password endpoint; the confirm endpoint is derived as `password_reset_url + "/confirm"`. |

On the auth instance, the default `rp_id` is the registrable domain (eTLD+1) of the forwarded host, so login pages on sibling subdomains of the auth host already share the passkey scope; set the `passkey` runtime settings (`rp_id`, `origins`) explicitly only when the login page lives on an unrelated domain. Keep `/auth/oauth2/*` publicly routable (don't chain `session` in front of the token/JWKS/passkey endpoints).

### Dynamic providers from the auth UI (`provider_source`)

Instead of (or on top of) the static `provider` map, the provider list can be
managed from the [`auth`](./auth) middleware's UI (the `session_providers`
settings namespace, *Federation → Session providers*). The session middleware
pulls that list at runtime and applies changes without a restart; a dynamic
provider overrides a same-named static one.

In-process, referencing the auth middleware by instance name:

```yaml
session:
  provider_source:
    auth_middleware: "auth"   # middleware key of the auth instance
  provider: {}                # optional static providers to merge under it
```

Or over HTTP from another turna instance:

```yaml
session:
  provider_source:
    url: https://auth.example.com/auth/v1/session-providers
    ttl: 30s
    headers:
      X-API-Key: "<admin api key>"
    insecure_skip_verify: false
```

| Field | Default | Description |
| --- | --- | --- |
| `auth_middleware` | | In-process auth middleware instance name. The list is read from its cache; a commit in the UI applies on the next request (no polling, no HTTP). Exactly one of `auth_middleware` or `url` must be set. |
| `url` | | `GET /v1/session-providers` endpoint of a remote auth middleware. The endpoint is admin-protected because the payload carries provider client secrets. |
| `ttl` | `30s` | Refresh interval for `url` sources. Between refreshes the last fetched list is served; a failed refresh keeps the last known list and backs off one TTL window. |
| `headers` | | Extra headers for `url` requests (authentication). |
| `insecure_skip_verify` | `false` | Skip TLS verification for `url` fetches. |

The token validation keyfunc is rebuilt only when the provider set actually
changes (new/removed providers, changed `cert_url` or `auth_middleware`);
claim mapping changes apply immediately without a rebuild.

### API key requests

When `api_key: true` is set on a provider, `session` checks the configured API key header after bearer-token validation and before cookie redirects. If present, the static key is validated directly — in-process via `auth_middleware`, or with a request to `oauth2.api_key_url` on a remote auth instance. On success the raw key header is deleted and the key principal's claims and `X-User: api-key:<id>` headers are set; no JWT is involved.

Validation hits the auth database on every request, so deleting or disabling a key (or its owner) cuts access immediately.

## Token Action

```yaml
action:
  active: token
  token:
    login_path: /login/
    disable_refresh: false
    insecure_skip_verify: false
    legacy_proxy_auth: false
    redirect_always: false
```

Bearer access tokens are validated directly. Session-stored access tokens are refreshed when they are within 10 seconds of expiry unless `disable_refresh` is true.

### Redirect vs 401 challenge

Only **interactive** anonymous requests are redirected to `login_path`: those whose `Accept` header offers HTML (`text/html`, `application/xhtml+xml`) or that carry a browser navigation header (`Sec-Fetch-Dest: document`). Everything else — curl, fetch/XHR without an HTML accept, MCP clients — answers **`401 Unauthorized`** with a `WWW-Authenticate: Bearer` challenge, which is what machine clients need to start an OAuth2 discovery flow instead of choking on a login page redirect.

Set `redirect_always: true` to restore the historic unconditional redirect for deployments whose clients depended on it.

Authentication failures on API-style requests (invalid bearer token, invalid API key, or `disable_redirect` routes) answer **`401 Unauthorized`** with a `WWW-Authenticate: Bearer` header. Set `legacy_proxy_auth: true` to restore the historic `407 Proxy Authentication Required` of the legacy `iam` stack for old deployments whose clients still expect 407.

## Protected resource metadata (RFC 9728)

Discovery-driven clients (the MCP spec) find the authorization server of a protected surface through the `WWW-Authenticate` challenge: `401` answers carry `resource_metadata="..."` pointing at a `/.well-known/oauth-protected-resource` document that lists the `authorization_servers`. Configure `protected_resource` to publish the session-protected surface that way:

```yaml
session:
  protected_resource:
    # Optional: require resource-bound aud only for these OAuth clients.
    # Empty or omitted disables audience enforcement.
    check_audience_azp:
      - https://claude.ai/oauth/claude-code-client-metadata
  provider:
    turna:
      auth_middleware: auth
  action:
    token:
      login_path: /login/
```

With the block present:

- Requests under `/.well-known/oauth-protected-resource` answer the metadata document without authentication, RFC 9728 path-insertion style: `/.well-known/oauth-protected-resource/krabby/mcp` describes the resource `https://{host}/krabby/mcp`.
- Every `401` challenge points at the metadata of the requested path: a challenge on `/krabby/mcp` answers `WWW-Authenticate: Bearer resource_metadata="https://{host}/.well-known/oauth-protected-resource/krabby/mcp"`.

Several MCP endpoints behind one session (`/krabby/mcp`, `/krabby/mcp/admin`, ...) therefore each get their own resource identifier and metadata URL without any per-endpoint configuration — both the challenge-driven discovery and clients that build the well-known URL themselves from the server URL (the MCP spec) land on the right document.

| Field | Default | Description |
| --- | --- | --- |
| `resource` | `{scheme}://{host}{path}` per request | Pins one canonical RFC 8707/9728 resource identifier for the whole surface (honors `X-Forwarded-Proto`/`X-Forwarded-Host` when deriving). Leave empty for per-path resources. |
| `authorization_servers` | derived | Issuer URLs listed in the metadata. Empty derives them from every provider backed by an in-process `auth_middleware` (the auth middleware's canonical issuer URL, honoring its `oauth2.base_url`). Set explicitly for remote/oauth2 providers. |
| `scopes_supported` | | Advertised scopes. |
| `check_audience_azp` | empty | OAuth client IDs (`azp` claims) whose bearer tokens must contain the exact requested resource identifier in `aud`. Matching is exact. Empty disables audience enforcement; unlisted clients continue to downstream `iam_check` normally. A listed client with a missing/wrong audience gets `401` + `error="invalid_token"` and the resource metadata pointer. |

A typical MCP flow behind session: the client calls the protected endpoint, gets `401` with the metadata pointer, reads `authorization_servers`, runs dynamic client registration + PKCE code flow against the auth middleware (keep its public plane reachable with `auth_skip_paths`), and retries with the bearer token. Session validates the token, conditionally checks its resource audience when its `azp` is listed, then sets `X-User`; a following `iam_check` still makes the user/path/method authorization decision.

## Skip paths

`skip_paths` lists request path patterns ([doublestar](https://github.com/bmatcuk/doublestar) globs) that never *require* authentication:

```yaml
session:
  skip_paths:
    - /auth/oauth2/**
    - /.well-known/**
  action:
    token:
      login_path: /login/
```

Behavior on a matched path:

- Credentials are still honored: a valid bearer token, API key, or session cookie authenticates the request as usual (claims context and `X-User` are set). This makes routes like the auth middleware's `/oauth2/consent` work — logged-in browsers get their identity, anonymous ones fall through to the page's own login redirect.
- Anonymous requests (or failed credentials) pass through **with all identity headers stripped** (`X-User`, `X-User-Id`, configured `claim_header`s) instead of being redirected or rejected — spoofing is not possible.

This keeps a single `[session, auth]` router while leaving the public OAuth2/MCP endpoints (`/oauth2/token`, `/oauth2/register`, `/oauth2/certs`, discovery documents, ...) reachable for machine clients.

### Auth skip paths

Hand-listing every public endpoint is error-prone — miss the federated callback (`/auth/oauth2/code/gitlab`) and the login popup bounces back to the login page; miss the token endpoint and the code exchange dies with `401 {"error":"Unauthorized"}` because session tries to parse the client's `Authorization: Basic` header as a bearer JWT.

Instead of enumerating them, list the in-process auth middleware by name in `auth_skip_paths`; the session pulls the issuer's published public plane into the skip set — for the auth middleware that is `<prefix>/oauth2/**`, `<prefix>/saml/**` and the root `/.well-known` discovery documents:

```yaml
session:
  auth_skip_paths:
    - auth  # name of the auth middleware instance
  action:
    token:
      login_path: /login/
```

Explicit `skip_paths` are still honored on top. Skip-path semantics stay the same: credentials are honored when present (so `/oauth2/consent` still sees `X-User`), anonymous requests pass through stripped.

Nothing is added implicitly: a provider's `auth_middleware` setting only wires token validation/refresh and has no effect on skip paths. When a session sits in front of an auth middleware, set `auth_skip_paths` (or list the patterns in `skip_paths` by hand) or the auth public plane gets captured by the login redirect.

Note: `/oauth2/consent` does **not** need to be excluded from skip paths — skipping makes authentication *optional*, not absent. A logged-in browser still gets its `X-User` on a skipped path, which is exactly what the consent page needs; anonymous visitors fall through to the consent page's own login redirect.

#### Public permission resources

An `auth_skip_paths` name also covers the auth middleware's [public permissions](./auth#public-permission-resources) — permissions flagged `public: true` in the auth UI/API. On every request the session runs an anonymous access check (host, path, method) against the named auth; a public match makes authentication optional with the usual skip semantics. Public addresses managed in the auth UI therefore apply live, without touching session config.

A public match additionally sets the `public_access` context flag, which a following [`iam_check`](./iam_check) reads to pass the request without running the same check again — the check is paid once per request, not twice. Explicit `skip_paths` pattern matches do not set the flag; `iam_check` still evaluates those requests.

When the auth middleware runs in another process, list its check endpoint URL instead of a name:

```yaml
session:
  auth_skip_paths:
    - auth                                   # in-process: static plane + public permissions
    - https://idp.example.com/auth/check     # remote: public permissions only
  action:
    token:
      login_path: /login/
```

A URL entry POSTs `{"host","path","method"}` anonymously to the remote auth's `<prefix>/check` endpoint (`200 {"allowed":true}` = public, `401` = not). URL entries contribute no static patterns — a remote auth's OAuth2 plane lives on the remote host. The check honors the request context with a 5s cap, uses `action.token.insecure_skip_verify` for TLS, and fails closed: an unreachable check endpoint means regular authentication applies.

## Context Flags

Use [`set`](./set) before `session` to change behavior for selected routes.

| Context key | Effect |
| --- | --- |
| `token_header` | For cookie-backed sessions, add `Authorization: Bearer <access_token>` before proxying. For direct bearer-token requests, remove the original header after validation. |
| `token_header_delete` | Delete the `Authorization` header before proxying. |
| `disable_redirect` | Return `401 Unauthorized` (or `407` with `legacy_proxy_auth`) instead of redirecting to `login_path`. |
| `cookie_name` | Override the session cookie name for this request. |

Session itself sets one flag for downstream middlewares: `public_access` is `true` when the request matched a permission flagged public on an `auth_skip_paths` auth ([`iam_check`](./iam_check) reads it to avoid a second check).
