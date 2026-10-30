# Single Page App

This example packages a frontend build together with Turna in one image. Turna serves the static files, keeps the login of the platform through a shared session, and asks the identity service from the [Auth example](/examples/auth) whether the user may open the app.

The platform is one host, `https://apps.example.com`, with a gateway in front:

| Path | Container | Serves |
| --- | --- | --- |
| `/auth/*` | `auth:8080` | Identity service, login page, OAuth2 endpoints |
| `/apps/reports/*` | `reports:8080` | This example |
| `/*` | `home:8080` | Landing page with a custom login page |

## Image

The Turna image only holds the binary at `/turna`, with `/` as working directory, so the frontend build can be added to it directly.

```dockerfile
FROM node:22-alpine AS build
WORKDIR /src
COPY . .
RUN corepack enable && pnpm install --frozen-lockfile && pnpm build

FROM ghcr.io/rakunlabs/turna:latest
COPY --from=build /src/dist /dist
```

The configuration is not baked into the image. The same image reads it from Consul in every environment:

```yaml
# compose.yaml
services:
  reports:
    image: registry.example.com/platform/reports:1.4.0
    environment:
      CONSUL_HTTP_ADDR: consul:8500
      CONSUL_CONFIG_PATH_PREFIX: platform/reports # reads platform/reports/turna
```

## Configuration

```yaml
# Consul key platform/reports/turna
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      session:
        session:
          cookie_name: platform_session
          session_key: change-me-32-byte-random-value
          store:
            active: redis
            redis:
              address: redis:6379
              key_prefix: platform_session_
          options:
            http_only: true
            secure: true
            same_site: 2
          action:
            active: token
            token:
              login_path: /auth/login/
          provider:
            company:
              password_flow: true
              oauth2:
                client_id: platform-ui
                cert_url: http://auth:8080/auth/oauth2/certs
                token_url: http://auth:8080/auth/oauth2/token

      check:
        iam_check:
          check_api: http://auth:8080/auth/check
          responses:
            - path: /apps/**
              redirect: /

      app:
        folder:
          path: ./dist
          index: true
          spa: true
          browse: false
          etag: weak
          disable_folder_slash_redirect: true
          file_path_regex:
            - regex: ^/apps/reports/?(.*)$
              replacement: /$1
          cache_regex:
            - regex: index\.html$
              cache_control: no-cache
            - regex: -[\w-]{8}\.(js|css)$
              cache_control: public, max-age=31536000, immutable

    routers:
      app:
        path: /*
        middlewares:
          - session
          - check
          - app
```

How it works:

- The gateway forwards `/apps/reports/...` without stripping the prefix, so the frontend can use `/apps/reports/` as its base URL. `file_path_regex` maps the request back to the build output: `/apps/reports/assets/main.js` reads `./dist/assets/main.js`, and unknown paths fall back to `index.html` because of `spa`.
- `session` uses the same `cookie_name`, `session_key` and Redis `key_prefix` as the identity service, so a user logged in at `/auth/login/` is logged in here too. Without a session the browser is sent to `/auth/login/` and comes back afterwards. Token validation and refresh use the internal `auth:8080` address.
- `check` calls the permission endpoint of the identity service with the user of the session. Users without a permission for this host and path are redirected to the landing page instead of seeing a `403`. Permissions are managed in the auth UI.
- `index.html` is never cached, so a new deployment is picked up on the next page load. Assets with a content hash in the file name (`main-AbC12_x9.js`, as Vite builds them) are cached for a year. `cache_regex` is matched against the file name, not the path.

To serve the app on its own host without the gateway, drop `file_path_regex` and set the redirect of `check` to an absolute URL.

## Landing page with a custom login page

The landing page owns the sign-in and sign-out pages of the platform. [`login`](/reference/server/http/middlewares/login) with `ui.external_folder` keeps the login API but lets the frontend render the page; the frontend calls the API with the [login SDK](/reference/server/http/middlewares/login#login-sdk).

```yaml
# Consul key platform/home/turna
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      session:
        session:
          # same session as above, only the login page differs
          cookie_name: platform_session
          session_key: change-me-32-byte-random-value
          store:
            active: redis
            redis:
              address: redis:6379
              key_prefix: platform_session_
          action:
            active: token
            token:
              login_path: /sign-in/
          provider:
            company:
              password_flow: true
              oauth2:
                client_id: platform-ui
                cert_url: http://auth:8080/auth/oauth2/certs
                token_url: http://auth:8080/auth/oauth2/token

      sign_in:
        login:
          session_middleware: session
          path:
            base: /sign-in/
          ui:
            external_folder: true

      sign_out:
        set:
          values:
            - logout

      home:
        folder:
          path: ./dist
          index: true
          spa: true
          strip_index_name: true
          etag: weak
          cache_regex:
            - regex: index\.html$
              cache_control: no-cache

    routers:
      assets:
        path:
          - /assets/*
          - /favicon.png
        middlewares:
          - home
      sign_in:
        path:
          - /sign-in
          - /sign-in/*
        middlewares:
          - sign_in
          - home
      sign_out:
        path: /sign-out
        middlewares:
          - sign_out
          - sign_in
          - home
      home:
        path: /*
        middlewares:
          - session
          - home
```

- `/assets/*` is served without a session, so the sign-in page can load its scripts and styles.
- `/sign-in/` is a page of the frontend. `login` answers its API under `/sign-in/auth/*` and passes the page request on to `home`, which falls back to `index.html`.
- `/sign-out` sets the `logout` flag, so `login` revokes the tokens, deletes the session and redirects back to the sign-in page.
- Everything else needs a session.
