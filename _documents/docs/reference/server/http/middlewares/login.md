# login

`login` provides a browser login UI and OAuth2 code/password flows. It stores received tokens through a configured [`session`](./session) middleware.

```yaml
server:
  http:
    middlewares:
      session:
        session:
          store:
            active: file
            file:
              session_key: my-secret-key
          provider:
            keycloak:
              password_flow: true
              oauth2:
                client_id: app
                cert_url: http://localhost:8080/realms/master/protocol/openid-connect/certs
                token_url: http://localhost:8080/realms/master/protocol/openid-connect/token
                auth_url: http://localhost:8080/realms/master/protocol/openid-connect/auth
          action:
            token:
              login_path: /login/
      login:
        login:
          session_middleware: session
          path:
            base: /login/
          redirect:
            schema: http
          info:
            title: Turna Login
```

## Fields

| Field | Description |
| --- | --- |
| `session_middleware` | Required session middleware instance name. |
| `path.base` | Base path for login UI and API routes. |
| `path.base_url` | Optional prefix used in login info responses. |
| `path.code` | Override code-flow route. Defaults to `{base}/auth/code`. |
| `path.token` | Override password-flow route. Defaults to `{base}/auth/token`. |
| `path.passkey` | Override passkey-flow route. Defaults to `{base}/auth/passkey`. |
| `path.methods` | Override the login-method manifest route. Defaults to `{base}/auth/methods`. |
| `path.info_ui` | Override provider-info route. Defaults to `{base}/auth/info/ui`. |
| `path.status` | Override status route. Defaults to `{base}/auth/status`. |
| `redirect.base_url` | Fixed external base URL for redirects. |
| `redirect.schema` | Default redirect scheme. Defaults to `https` unless forwarded headers are set. |
| `ui.external_folder` | Forward GET UI requests to the next middleware instead of serving embedded UI. |
| `info.title` | Login UI title. |
| `request.insecure_skip_verify` | Skip TLS verification for token requests. |
| `state_cookie` | Cookie settings for OAuth2 state. |
| `success_cookie` | Cookie settings for login success marker. |
| `store` | Temporary code/state store. Empty means memory; `active: redis` uses Redis. |
| `redirect_white_list` | Allowed redirect URI prefixes when minting internal codes. Empty allows all. |

## Default Routes

For `path.base: /login/`, default routes are:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/login/auth/methods` | Available password, OAuth and passkey methods plus login-page metadata. Canonical endpoint used by the embedded UI. |
| `GET` | `/login/auth/code/{provider}` | Start or finish OAuth2 authorization code flow. |
| `POST` | `/login/auth/token/{provider}` | Password flow token login. |
| `POST` | `/login/auth/passkey/{provider}` | WebAuthn (passkey) begin/finish ceremony; works with providers backed by an in-process [`auth`](./auth) middleware (`auth_middleware` + `passkey: true`). |
| `POST` | `/login/auth/signup/{provider}` | Self-registration proxy to the auth middleware (when its `signup` setting is enabled). |
| `POST` | `/login/auth/signup/verify/{provider}` | Email verification code confirmation. |
| `POST` | `/login/auth/reset/{provider}` | Forgot-password mail request. |
| `POST` | `/login/auth/reset/confirm/{provider}` | Set a new password with a reset code. |
| `GET` | `/login/auth/info/ui` | Provider list for the UI. |
| `GET` | `/login/auth/status` | Login status endpoint. |

`/login/auth/info/ui` and `/login/?auth_info=true` remain compatibility aliases for the methods response. New integrations should use `/login/auth/methods`. All methods responses carry `Cache-Control: no-store` because provider and self-service capabilities can change at runtime.

The canonical endpoint uses the standard payload envelope:

```json
{
  "payload": {
    "title": "Login",
    "provider": {
      "password": [],
      "code": [],
      "passkey": []
    }
  }
}
```

The two compatibility aliases keep their historic unwrapped `{title, provider}` response.

## Remember me

The embedded login page exposes one **Remember me** choice shared by password, passkey and authorization-code buttons. The login middleware carries `remember_me=true` to the provider's token mint; it does not calculate token lifetimes itself.

The built-in [`auth`](./auth) issuer treats an unchecked login as a fixed refresh session (default `24h`). A checked login uses a sliding refresh window (default `24h`) up to `token.refresh_absolute_lifetime` (default `720h`, 30 days). The choice is signed into the refresh token and cannot be turned on later by adding a field to a refresh request. External OAuth providers may ignore this Turna extension.

## Passkey

When a session provider sets `passkey: true` together with `auth_middleware` (in-process) or `oauth2.passkey_url` (remote auth instance), the login UI shows a "Sign in with a passkey" button. The login middleware proxies WebAuthn begin/finish payloads to the auth middleware — in-process or over HTTP with the original host/scheme forwarded — injects the provider's `client_id`/`client_secret`/`scopes`, and stores the issued tokens in the session on success. Passkeys are registered through the auth middleware (`/auth/v1/passkey/register`, available in its management UI).

## Signup and forgot password

When a password provider is backed by an [`auth`](./auth) middleware whose `signup` runtime setting enables self-registration and/or password reset, the login page automatically shows "Create account" and "Forgot password?" links — no login configuration needed, and toggling the settings in the auth UI applies live. For a remote auth instance set `oauth2.signup_url` / `oauth2.password_reset_url` on the provider instead.

The login middleware proxies these requests and injects the provider's client credentials, so the browser never sees the client secret. Mails carry a one-time code and, when the login page URL passes the OAuth client's `whitelist_urls`, a magic link back to the login page (`?flow=verify&code=...` / `?flow=reset&code=...`) that prefills the matching form.

## Logout

Set the `logout` context value before `login` to delete the session and redirect through the login middleware.

Before the session is deleted, the stored refresh and access tokens are best-effort **revoked at the issuer** so they cannot be replayed: in-process when the provider uses `auth_middleware` (the [`auth`](./auth) middleware's RFC 7009 denylist), or over `oauth2.revocation_url` for remote providers. When the provider has an `oauth2.logout_url` (OIDC RP-initiated logout), it is called with `id_token_hint` as before.

```yaml
server:
  http:
    middlewares:
      logout_flag:
        set:
          values:
            - logout
    routers:
      logout:
        path: /logout/*
        middlewares:
          - logout_flag
          - login
```
