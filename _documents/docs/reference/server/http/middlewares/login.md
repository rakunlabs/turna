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
| `info.disable_remember_me` | Hide the remember-me choice on the login page and ignore `remember_me` in every flow; all sign-ins become standard sessions. Defaults to `false`. |
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

## Custom login UI

Set `ui.external_folder: true` to keep the login API routes but serve your own HTML, CSS and JavaScript through the next middleware. For example, this serves a single-page application from `./login-ui`:

```yaml
server:
  http:
    middlewares:
      login:
        login:
          session_middleware: session
          path:
            base: /login/
          ui:
            external_folder: true

      custom_login_ui:
        folder:
          path: ./login-ui
          prefix_path: /login/
          index: true
          spa: true

    routers:
      login:
        path: /login/*
        middlewares:
          - login
          - custom_login_ui
```

The `login` middleware handles its reserved `/login/auth/*` routes first and forwards other `GET` requests to `custom_login_ui`. Keep custom assets outside the `auth` subpath.

### Discover the available methods

Do not hard-code provider names or endpoint URLs. Fetch the method manifest when the page loads:

```ts
type LoginLink = {
  name: string
  url: string
  signup_url?: string
  signup_verify_url?: string
  password_reset_url?: string
  password_reset_confirm_url?: string
  password_min_length?: number
}

type LoginMethods = {
  title: string
  disable_remember_me?: boolean
  provider: {
    password: LoginLink[] | null
    code: LoginLink[] | null
    passkey: LoginLink[] | null
  }
}

const response = await fetch('/login/auth/methods', {
  credentials: 'same-origin',
})
if (!response.ok) throw new Error('Cannot load login methods')

const { payload: methods }: { payload: LoginMethods } = await response.json()
const passwordMethods = methods.provider.password ?? []
const codeMethods = methods.provider.code ?? []
const passkeyMethods = methods.provider.passkey ?? []
```

Each item is a login choice. Render `name` as its label and send the flow to its `url`. Use the link object itself as the selected value; `name` is a display label and is not guaranteed to be unique. The optional signup and reset URLs only appear when that provider supports those features.

The following helper handles the standard `{message, error}` error response and the OAuth-shaped errors that a proxied provider may return:

```ts
async function postJSON(url: string, body: unknown) {
  const response = await fetch(url, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

  if (!response.ok) {
    const problem = await response.json().catch(() => ({}))
    throw new Error(
      problem.message ?? problem.error_description ?? problem.error ?? 'Sign-in failed',
    )
  }

  return response.status === 204 ? undefined : response.json()
}
```

### Password flow

POST the credentials to the selected password link. A successful request returns `204`; the login middleware stores the provider tokens and sets the configured session cookie.

```ts
async function signInWithPassword(
  provider: LoginLink,
  username: string,
  password: string,
  rememberMe: boolean,
) {
  await postJSON(provider.url, {
    username,
    password,
    remember_me: rememberMe,
  })

  finishLogin()
}
```

### Authorization code flow

Open the selected code link in a popup. Add `remember_me=true` before opening it; the middleware binds the choice to the OAuth state and uses it when the callback exchanges the code.

```ts
function signInWithCode(provider: LoginLink, rememberMe: boolean) {
  const target = new URL(provider.url, window.location.origin)
  if (rememberMe) target.searchParams.set('remember_me', 'true')

  const popup = window.open(target, 'turna-login', 'width=520,height=720')
  if (!popup) throw new Error('The sign-in popup was blocked')

  let timer = 0
  const cleanup = () => {
    window.removeEventListener('message', onMessage)
    window.clearInterval(timer)
  }
  const complete = () => {
    cleanup()
    popup.close()
    finishLogin()
  }
  const onMessage = (event: MessageEvent) => {
    if (
      event.origin === window.location.origin &&
      event.data === 'turna:login:success'
    ) complete()
  }

  window.addEventListener('message', onMessage)
  timer = window.setInterval(() => {
    // The short-lived, non-HttpOnly marker is the fallback when an upstream
    // provider's COOP policy disconnects window.opener.
    const succeeded = document.cookie
      .split('; ')
      .some((cookie) => cookie === 'auth_verify=true')
    if (succeeded) complete()
  }, 500)
}
```

The callback page sends `turna:login:success` and also sets the short-lived `auth_verify=true` marker. Check the message origin exactly as shown. Polling the marker makes the flow work with providers whose Cross-Origin-Opener-Policy severs `window.opener`.

### Passkey flow

Passkey login is a two-request WebAuthn ceremony. The first request returns a `session_id` and browser credential options. Convert the base64url fields to byte arrays, call `navigator.credentials.get`, then return the serialized assertion in the second request:

```ts
function fromBase64URL(value: string): Uint8Array {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64.padEnd(base64.length + ((4 - base64.length % 4) % 4), '=')
  return Uint8Array.from(atob(padded), (char) => char.charCodeAt(0))
}

function toBase64URL(value: ArrayBuffer | null): string | undefined {
  if (!value) return undefined
  const bytes = new Uint8Array(value)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

async function signInWithPasskey(
  provider: LoginLink,
  username: string,
  rememberMe: boolean,
) {
  if (!window.PublicKeyCredential) throw new Error('Passkeys are not supported')

  const begin = await postJSON(provider.url, {
    ...(username ? { username } : {}),
    remember_me: rememberMe,
  })
  const options = begin.options

  const credential = await navigator.credentials.get({
    publicKey: {
      ...options,
      challenge: fromBase64URL(options.challenge),
      allowCredentials: (options.allowCredentials ?? []).map((item: any) => ({
        ...item,
        id: fromBase64URL(item.id),
      })),
    },
  }) as PublicKeyCredential | null
  if (!credential) throw new Error('Passkey sign-in was cancelled')

  const assertion = credential.response as AuthenticatorAssertionResponse
  await postJSON(provider.url, {
    session_id: begin.session_id,
    remember_me: rememberMe,
    assertion: {
      id: credential.id,
      rawId: toBase64URL(credential.rawId),
      type: credential.type,
      response: {
        clientDataJSON: toBase64URL(assertion.clientDataJSON),
        authenticatorData: toBase64URL(assertion.authenticatorData),
        signature: toBase64URL(assertion.signature),
        userHandle: toBase64URL(assertion.userHandle),
      },
    },
  })

  finishLogin()
}
```

An empty `username` starts a discoverable, username-less passkey flow. Passkeys require a secure browser context (HTTPS, with the usual localhost exception). Send the same `remember_me` value on both passkey requests; the finish request controls the issued session.

### Finish the login

After password or passkey returns `204`, or the code popup reports success, navigate to the requested application path. When the login page is being used as part of Turna's own authorization-code flow, reload it instead so the middleware can return the pending code.

```ts
function finishLogin() {
  const query = new URLSearchParams(window.location.search)
  if (query.get('response_type') === 'code') {
    window.location.reload()
    return
  }

  const requested = query.get('redirect_path') ?? '/'
  const target = new URL(requested, window.location.origin)
  const targetPath = target.pathname.replace(/\/+$/, '') || '/'
  const loginPath = window.location.pathname.replace(/\/+$/, '') || '/'
  const safeTarget = target.origin === window.location.origin &&
    !requested.startsWith('//') && targetPath !== loginPath
    ? `${target.pathname}${target.search}${target.hash}`
    : '/'
  window.location.assign(safeTarget)
}
```

Serve the custom page and login endpoints from the same origin unless you explicitly add and audit a CORS layer. Never expose the provider `client_secret` or store returned tokens in browser storage; the middleware injects provider credentials server-side and owns the session cookie.

## Remember me

The embedded login page exposes one **Remember me** choice shared by password, passkey and authorization-code buttons. The login middleware carries `remember_me=true` to the provider's token mint; it does not calculate token lifetimes itself.

The built-in [`auth`](./auth) issuer treats an unchecked login as a fixed refresh session (default `24h`). A checked login uses a sliding refresh window (default `24h`) up to `token.refresh_absolute_lifetime` (default `720h`, 30 days). The choice is signed into the refresh token and cannot be turned on later by adding a field to a refresh request. External OAuth providers may ignore this Turna extension.

Setting `info.disable_remember_me: true` removes the choice: the checkbox disappears from the embedded page (the methods response advertises `disable_remember_me: true`), and the middleware forces `remember_me` off in the password, passkey and code flows even if a client sends it explicitly.

Custom login UIs should use the same field from `GET /login/auth/methods`: hide the choice when `payload.disable_remember_me` is `true`. The field is omitted when it is `false`, so a missing value means the choice may be shown. The compatibility aliases expose the field at the response root instead of under `payload`.

Use one checkbox for every method and derive its effective value before starting a flow:

```ts
const effectiveRememberMe = !methods.disable_remember_me && rememberCheckbox.checked
```

Do not implement "remember me" by saving the username, password or tokens in `localStorage`. It is a token-lifetime choice sent to the issuer; the session cookie and token storage remain owned by the `session` middleware.

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
