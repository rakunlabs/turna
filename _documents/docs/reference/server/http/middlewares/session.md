# session

`session` validates bearer tokens or access tokens stored in a server-side session. It also sets identity headers for later middlewares and upstream services.

```yaml
server:
  http:
    middlewares:
      session:
        session:
          cookie_name: auth_session
          store:
            active: file
            file:
              session_key: my-secret-key
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
store:
  active: redis # redis or file
  redis:
    address: localhost:6379
    username: ""
    password: ""
    key_prefix: session_
    session_key: ""
    tls:
      enabled: false
      cert_file: ""
      key_file: ""
      ca_file: ""
  file:
    session_key: ""
    path: ""
```

If `active` is empty, Turna uses `redis` when configured, otherwise `file` when configured. A store is required.

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

On the auth instance, set the `passkey` runtime settings (`rp_id`, `origins`) explicitly when the login page is served from a different domain than the auth host, and keep `/auth/oauth2/*` publicly routable (don't chain `session` in front of the token/JWKS/passkey endpoints).

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
```

Bearer access tokens are validated directly. Session-stored access tokens are refreshed when they are within 10 seconds of expiry unless `disable_refresh` is true.

## Context Flags

Use [`set`](./set) before `session` to change behavior for selected routes.

| Context key | Effect |
| --- | --- |
| `token_header` | For cookie-backed sessions, add `Authorization: Bearer <access_token>` before proxying. For direct bearer-token requests, remove the original header after validation. |
| `token_header_delete` | Delete the `Authorization` header before proxying. |
| `disable_redirect` | Return `407 Proxy Authentication Required` instead of redirecting to `login_path`. |
| `cookie_name` | Override the session cookie name for this request. |
