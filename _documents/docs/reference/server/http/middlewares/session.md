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
