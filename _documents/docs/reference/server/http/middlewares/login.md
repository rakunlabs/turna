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
| `GET` | `/login/auth/sdk.js` | Login SDK ES module for custom login pages. Served with `Cache-Control: no-cache`, also when `ui.external_folder` is enabled. |
| `GET` | `/login/auth/sdk.d.ts` | Bundled TypeScript declarations for the SDK, for download into custom login UI projects. |

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
| `code(link, {rememberMe?, target?, features?, signal?, onPopupClosed?})` | OAuth2 popup flow. Resolves on success via the `turna:login:success` message or the `auth_verify` cookie fallback (for COOP providers); rejects when the popup is blocked or `signal` aborts. `onPopupClosed` is a one-shot hint for showing a "window closed?" message while the flow keeps waiting. |
| `signup(link, {email, password, name?, redirectUri?})` | Self-registration; returns `{message, verificationRequired}`. |
| `signupVerify(link, code)` / `resetRequest(link, {email})` / `resetConfirm(link, code, password)` | Email verification and forgot-password flows. |
| `finish()` | Safe post-login navigation: reloads inside Turna's own authorization-code flow, otherwise follows the validated `redirect_path` query parameter. |
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

Every SDK operation is a plain HTTP endpoint (see [Default Routes](#default-routes)); non-browser clients can call them directly. In short: the password and passkey flows return `204` on success and set the session cookie; the passkey flow is a begin/finish pair carrying `session_id`, base64url-encoded WebAuthn `options` and `assertion`; the code-flow callback page posts `turna:login:success` to its opener and sets the short-lived, non-HttpOnly `auth_verify=true` cookie. Error responses use the `{message, error}` envelope, possibly wrapping an OAuth2 error body. The SDK source (`pkg/server/http/middleware/login/_ui/src/sdk/index.ts`) is the reference implementation.

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
