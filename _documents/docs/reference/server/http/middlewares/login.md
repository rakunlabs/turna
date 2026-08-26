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

### Internal authorization codes

A logged-in `GET {base}?response_type=code&client_id=...&redirect_uri=...` mints an internal authorization code in `store` and redirects back with `?code=&state=`. The code is bound to the request's `client_id` and `redirect_uri` and carries `nonce` plus any `code_challenge`/`code_challenge_method` (RFC 7636); the [`auth`](./auth) middleware token endpoint verifies those bindings and rejects unbound codes with `code was issued to another client`. Send the same `client_id` and `redirect_uri` at the exchange.

This matters when one login page is used as the provider of another (nested login windows): the outer login must forward `client_id` and redeem with the same value, and both sides must share the same code store as the auth middleware for the code to be found at all. `redirect_white_list` should be set whenever codes are minted this way — an empty list accepts every redirect target.

### Custom paths and nested bases

`path.base` may live anywhere, including nested under another middleware's prefix (e.g. `path.base: /auth/login/` next to an [`auth`](./auth) middleware at `/auth`). All reserved routes, the embedded UI and the SDK derive from the base relatively, so no further configuration is needed — but register the router for both the slashless and wildcard forms, otherwise the exact `/auth/login` request falls through to the broader `/auth/*` router instead of reaching the login page:

```yaml
routers:
  login:
    path:
      - /auth/login
      - /auth/login/*
    middlewares: [login]
  auth:
    path: /auth/*
    middlewares: [session, auth]
```

Route overrides (`path.code`, `path.token`, `path.passkey`, ...) are advertised through the methods manifest, so SDK-based and custom UIs pick them up automatically. An overridden `path.methods` is injected into the served `sdk.js` itself, keeping `login.methods()` working without client-side configuration; the default `{base}/auth/methods` route keeps answering alongside the override, so the embedded login page (which bundles the SDK at build time and always calls the default path) is unaffected.

When the attached session uses an in-process `provider_source.auth_middleware`, `GET {base}/auth/methods/{group}` returns the same sanitized manifest for one Auth-managed session-provider group. Auth changes apply on the next request. The endpoint only controls presentation: `provider_source.group` remains the session's effective flow and token-validation set, so a manifest from another group may be displayed but its providers are not accepted by that session. Unknown groups return `404`; provider configuration and client secrets are never included.

## Default Routes

For `path.base: /login/`, default routes are:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/login/auth/methods` | Available password, OAuth and passkey methods plus login-page metadata. Canonical endpoint used by the embedded UI. |
| `GET` | `/login/auth/methods/{group}` | Methods from one Auth-managed provider group; available for in-process provider sources. |
| `GET` | `/login/auth/code/{provider}` | Start or finish OAuth2 authorization code flow. |
| `POST` | `/login/auth/token/{provider}` | Password flow token login. |
| `POST` | `/login/auth/passkey/{provider}` | WebAuthn (passkey) begin/finish ceremony; works with providers backed by an in-process [`auth`](./auth) middleware (`auth_middleware` + `passkey: true`). |
| `POST` | `/login/auth/signup/{provider}` | Self-registration proxy to the auth middleware (when its `signup` setting is enabled). |
| `POST` | `/login/auth/signup/verify/{provider}` | Email verification code confirmation. |
| `POST` | `/login/auth/reset/{provider}` | Forgot-password mail request. |
| `POST` | `/login/auth/reset/confirm/{provider}` | Set a new password with a reset code. |
| `GET` | `/login/auth/info/ui` | Provider list for the UI. |
| `GET` | `/login/auth/status` | Login status endpoint. |
| `GET` | `/login/auth/sdk.js` | Login SDK ES module for custom login pages. Served with `Cache-Control: no-cache`, also when `ui.external_folder` is enabled. |
| `GET` | `/login/auth/sdk.d.ts` | Bundled TypeScript declarations for the SDK, for download into custom login UI projects. |

`/login/auth/info/ui` and `/login/?auth_info=true` remain compatibility aliases for the methods response. New integrations should use `/login/auth/methods`. All methods responses carry `Cache-Control: no-store` because provider and self-service capabilities can change at runtime.

The canonical endpoint uses the standard payload envelope. A response with one password provider (backed by an [`auth`](./auth) middleware with signup, password reset and passkeys enabled) and one external code provider looks like:

```json
{
  "payload": {
    "title": "Turna Login",
    "provider": {
      "password": [
        {
          "name": "Auth",
          "url": "/login/auth/token/auth",
          "signup_url": "/login/auth/signup/auth",
          "signup_verify_url": "/login/auth/signup/verify/auth",
          "password_reset_url": "/login/auth/reset/auth",
          "password_reset_confirm_url": "/login/auth/reset/confirm/auth",
          "password_min_length": 12
        }
      ],
      "code": [
        {
          "name": "Keycloak",
          "url": "/login/auth/code/keycloak"
        }
      ],
      "passkey": [
        {
          "name": "Auth",
          "url": "/login/auth/passkey/auth"
        }
      ]
    }
  }
}
```

Each link is one login choice: `name` is a display label (not guaranteed to be unique) and `url` is the endpoint to use for that flow — always take it from the manifest instead of hard-coding routes, so `path.*` overrides and `path.base_url` keep working. The `signup_*`, `password_reset_*` and `password_min_length` fields appear only on password providers whose auth middleware enables those features. `disable_remember_me: true` is added next to `title` when the remember-me choice must be hidden; the field is omitted when false. Provider groups without any entry may be `null`.

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

### Login SDK

The middleware serves its complete flow logic as a framework-agnostic, zero-dependency ES module at the reserved `GET {base}/auth/sdk.js` route — also when `ui.external_folder` is enabled. The embedded login page is built on the same module, so a custom page gets identical behavior without reimplementing anything: method discovery, password login, the full WebAuthn passkey ceremony, the OAuth2 popup flow with its COOP fallback, signup, email verification, password reset and the safe post-login redirect.

```html
<script type="module">
  import { createLogin } from "/login/auth/sdk.js";

  const login = createLogin(); // base path derived from the module URL
  const methods = await login.methods();

  // Render your own markup from the manifest, then wire the flows.
  // Each link object is a login choice; `name` is a display label and is
  // not guaranteed to be unique.
  const provider = methods.provider.password?.[0];

  document.querySelector("#form").addEventListener("submit", async (event) => {
    event.preventDefault();
    try {
      await login.password(provider, {
        username: event.target.username.value,
        password: event.target.password.value,
        rememberMe: event.target.remember.checked,
      });
      login.finish();
    } catch (err) {
      show(err.message); // LoginError with a normalized, user-safe message
    }
  });
</script>
```

| Member | Description |
| --- | --- |
| `createLogin({base?})` | Create a client. The base path is derived from the SDK module URL; pass `base` (e.g. `"/login/"`) when bundling the SDK source yourself. |
| `methods()` | Fetch the login method manifest (`GET {base}/auth/methods`, payload unwrapped). Do not hard-code provider names or URLs. |
| `password(link, {username, password, rememberMe?, extra?})` | Password flow. Resolves when the session cookie is set. |
| `passkey(link, {username?, rememberMe?})` | Two-request WebAuthn ceremony, including all base64url conversions. Omitting `username` starts the discoverable, username-less flow: the browser lists resident passkeys and the user picks an account. |
| `code(link, {rememberMe?, target?, features?, signal?, onPopupClosed?})` | OAuth2 popup flow. Each call mints a flow id, sends it as `turna_flow`, and waits for the matching `auth_verify_<flow>` cookie, so a nested sign-in inside the popup can never complete this call. A message from the exact popup triggers the authoritative cookie check; polling the same cookie remains the COOP fallback. Rejects when the popup is blocked or `signal` aborts. `onPopupClosed` is a one-shot hint for showing a "window closed?" message while the flow keeps waiting. `features` defaults to a centered popup *window* — browsers ignore scripted focus on background tabs, so only closing a real popup window reliably brings the opener back to front; pass `features: ""` to open a tab instead. |
| `signup(link, {email, password, name?, redirectUri?})` | Self-registration; returns `{message, verificationRequired}`. |
| `signupVerify(link, code)` / `resetRequest(link, {email})` / `resetConfirm(link, code, password)` | Email verification and forgot-password flows. |
| `finish()` | Safe post-login navigation: reloads inside Turna's own authorization-code flow, otherwise follows the validated `redirect_path` query parameter, and only with nothing left to follow relays verified completion to a same-origin opener and closes. A pending `redirect_path` always wins: an intermediate login page in a nested flow is a popup that still has an authorize URL to walk back through. |
| `isWebAuthnSupported()`, `flowFromURL()`, `getRedirectPath()`, `isResponseTypeCode()`, `LoginError` | Helpers: hide passkey buttons on unsupported browsers, prefill verify/reset forms from mail magic links (`?flow=...&code=...`), and branch on normalized errors (`status`, `credentials`). |

Honor `methods.disable_remember_me`: hide the remember-me choice when it is true (the server forces `remember_me` off anyway).

The SDK is versioned with the Turna binary, so it always matches the server endpoints — prefer loading it from the served URL over vendoring the source. It is served with `Cache-Control: no-cache`, so an upgraded Turna delivers the matching SDK on the next page load. The exported surface is a stable contract; a breaking change would be published under a new file name.

### Development workflow

The SDK and the session cookie are same-origin. During development, run your UI dev server with a proxy to the Turna instance instead of calling it cross-origin:

```js
// vite.config.js of the custom login page project
export default {
  server: {
    proxy: {
      "/login": "http://localhost:8080", // turna
    },
  },
}
```

Load the SDK through the browser, not through the bundler, so the same URL works in development (via the proxy) and in production:

```ts
// inside a bundled app: keep the import at runtime
const { createLogin } = await import(/* @vite-ignore */ "/login/auth/sdk.js");
```

or use a plain `<script type="module">` import as in the example above.

### TypeScript and linting

The compiler and linters cannot resolve a URL import by themselves. The middleware serves the SDK's bundled, self-contained type declarations at `GET {base}/auth/sdk.d.ts`; download them into the project and map the URL specifier onto them:

```sh
curl -o src/types/turna-login-sdk.d.ts http://localhost:8080/login/auth/sdk.d.ts
```

```jsonc
// tsconfig.json
{
  "compilerOptions": {
    "paths": {
      "/login/auth/sdk.js": ["./src/types/turna-login-sdk.d.ts"]
    }
  }
}
```

With the mapping in place a normal static import type-checks, and `eslint-import-resolver-typescript` resolves it for `import/no-unresolved`:

```ts
import { createLogin, LoginError, type LoginMethods } from "/login/auth/sdk.js";
```

Keep the import out of the production bundle so the browser loads the served SDK at runtime — for a static import, mark the specifier external:

```js
// vite.config.js
export default {
  build: {
    rollupOptions: {
      external: ["/login/auth/sdk.js"],
    },
  },
}
```

In development Vite leaves root-absolute imports to the browser, so the request flows through the `/login` dev proxy. The dynamic `import(/* @vite-ignore */ ...)` form needs no external config; type it with the same declarations: `const sdk: typeof import("/login/auth/sdk.js") = await import(/* @vite-ignore */ "/login/auth/sdk.js")`.

Refresh the downloaded declaration file when upgrading Turna (the additions are backward compatible within `sdk.js`).

### Raw API

Every SDK operation is a plain HTTP endpoint (see [Default Routes](#default-routes)); non-browser clients and custom UIs that do not use the SDK can call them directly. The SDK source (`pkg/server/http/middleware/login/_ui/src/sdk/index.ts`) is the reference implementation.

Every implementation starts the same way: fetch `GET {base}/auth/methods`, render one control per link in `payload.provider.password` / `.code` / `.passkey`, and run the flow for the link the user picks against that link's `url`. Error responses of all flows use the `{message, error}` envelope, possibly wrapping an OAuth2 `{error, error_description}` body. After any successful flow the session cookie is already set; finish by navigating like the SDK's `finish()`: reload the page when `?response_type=code` is present (Turna's own authorization-code flow), otherwise follow a validated same-origin `redirect_path` query parameter.

#### Password

`POST` the credentials as JSON to the link's `url`:

```http
POST /login/auth/token/auth
Content-Type: application/json

{"username": "demo", "password": "secret", "remember_me": true}
```

Success is `204 No Content` with the session cookie set. On failure, collapse the credential-mismatch details (`password not match`, `user not found`, `secret not match`, or a plain `401`) into one neutral "invalid username or password" message so the page never reveals which part was wrong.

#### Code

Open the link's `url` in a popup (or navigate top-level), appending `?remember_me=true` when the choice is checked and `?turna_flow=<id>` to identify this popup:

```
GET /login/auth/code/keycloak?remember_me=true&turna_flow=6f1c...
```

The middleware answers with a `307` redirect to the provider's authorization URL and later receives the callback (`?code=&state=`) on the same route. The callback stores the tokens in the session and serves a small success page that posts the `turna:login:success` message to `window.opener` (same origin). The message is only a trigger: accept it from the exact popup and verify the cookie before completing. Keep polling the cookie as a fallback because COOP-enabled providers can sever the `window.opener` handle and may even make the popup report `closed` while sign-in is still in progress. Never reject solely because the popup looks closed.

Both cookies backing this flow are scoped to a single sign-in attempt, because a popup may itself be a login page that opens further popups:

- The `auth_state` CSRF cookie is stored under a state-derived name. A shared name let a nested flow overwrite the outer state and delete it on consumption, so the outer callback failed with `state is not valid` and left the popup stranded on an error page.
- The short-lived, non-HttpOnly completion marker is `auth_verify_<turna_flow>=true`, mirroring the `turna_flow` value the popup was opened with (`[A-Za-z0-9_-]`, up to 64 characters; anything else falls back to the shared `auth_verify` name). A shared marker let the innermost sign-in satisfy every waiting opener at once: the outermost login page resolved early, closed the intermediate window mid-redirect and navigated away without ever getting a session.

Wait only for your own `auth_verify_<flow>` cookie, and let each level complete its own callback. Focus returns naturally: the success page focuses its opener before closing.

#### Passkey

A begin/finish pair of `POST`s to the same link `url`. Begin carries no `assertion`; omitting `username` starts the discoverable (username-less) flow where the browser offers every resident passkey:

```http
POST /login/auth/passkey/auth
Content-Type: application/json

{"username": "demo", "remember_me": false}
```

```json
{
  "session_id": "8Zl2mJ4v...",
  "options": {
    "challenge": "1nZFbXVK...",
    "timeout": 60000,
    "rpId": "example.com",
    "allowCredentials": [
      {"type": "public-key", "id": "kFum-Ci0...", "transports": ["internal", "hybrid"]}
    ],
    "userVerification": "preferred"
  }
}
```

All binary fields are URL-safe base64 without padding. Decode `challenge` and each `allowCredentials[].id` into buffers, run `navigator.credentials.get({publicKey})`, then base64url-encode the assertion buffers and finish with the same `session_id`:

```http
POST /login/auth/passkey/auth
Content-Type: application/json

{
  "session_id": "8Zl2mJ4v...",
  "remember_me": false,
  "assertion": {
    "id": "kFum-Ci0...",
    "rawId": "kFum-Ci0...",
    "type": "public-key",
    "response": {
      "clientDataJSON": "eyJ0eXBl...",
      "authenticatorData": "SZYN5Ygh...",
      "signature": "MEUCIQD...",
      "userHandle": "YWxpY2U"
    }
  }
}
```

Success is `204 No Content` with the session cookie set; the `remember_me` value of the **finish** request controls the issued session. The begin session is one-shot — a failed finish needs a new begin.

Serve the custom page and login endpoints from the same origin unless you explicitly add and audit a CORS layer. Never expose the provider `client_secret` or store returned tokens in browser storage; the middleware injects the provider's `client_id`/`client_secret`/`scope` server-side in every flow and owns the session cookie.

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
