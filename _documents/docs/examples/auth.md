# Auth

This example runs the PostgreSQL-backed [`auth`](/reference/server/http/middlewares/auth) middleware as the identity service of a platform, with [`session`](/reference/server/http/middlewares/session) and [`login`](/reference/server/http/middlewares/login) in front for browser authentication. Other applications share the login through the same session cookie and ask this service for permissions, see [Single Page App](/examples/spa).

Requirements: a reachable PostgreSQL database (migrations run automatically), a Redis compatible store shared by every Turna that reads the session, and an encryption key.

## Identity service

The configuration below runs in its own container, reachable as `auth:8080` inside the platform network and published as `https://apps.example.com/auth/`.

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      auth:
        auth:
          prefix_path: /auth
          database:
            dsn: postgres://auth_app:change-me@postgres:5432/platform?search_path=auth&application_name=auth
          encryption:
            key: change-me-encryption-key

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
            same_site: 2 # lax
          auth_skip_paths:
            - auth # keeps /auth/oauth2/** and /.well-known/** public
          provider:
            company:
              auth_middleware: auth # in-process token validation and refresh
              password_flow: true
              passkey: true
              oauth2:
                client_id: platform-ui # OAuth client registered in auth
                scopes: [openid]
          set_provider: company
          action:
            active: token
            token:
              login_path: /auth/login/

      login:
        login:
          session_middleware: session
          path:
            base: /auth/login/
          info:
            title: Example Platform
          store:
            active: redis
            redis:
              address: redis:6379

      logout:
        set:
          values:
            - logout

    routers:
      auth:
        path:
          - /auth/*
          - /.well-known/*
        middlewares:
          - session # sets X-User for the auth API and UI
          - auth
      login:
        path: /auth/login/*
        middlewares:
          - login
      logout:
        path: /auth/logout/*
        middlewares:
          - logout
          - login
      check:
        host: auth # only reachable inside the platform network
        path:
          - /auth/check
        middlewares:
          - auth
```

This document holds the database password and the keys, so keep it in Vault rather than in the image: store it as the secret `platform/auth` and start the container with `VAULT_ADDR`, `VAULT_SECRET_BASE_PATH`, `CONFIG_NAME_PREFIX=platform` and `APP_NAME=auth`. See [Config from Consul or Vault](/examples/wrap#config-from-consul-or-vault).

How the pieces fit:

- `auth` serves the IAM API, OAuth2/OIDC endpoints and the embedded management UI under `/auth`. Runtime settings (token lifetimes, clients, providers, LDAP, TOTP, passkeys, ...) live in PostgreSQL and are managed through `/auth/ui/`.
- `session` references the auth middleware by name (`auth_middleware`), so JWT validation and token refresh happen in-process without HTTP self-calls. `auth_skip_paths` keeps the public OAuth2 plane and the public permissions of `auth` reachable without a login.
- `login` renders the login page at `/auth/login/`. `/auth/logout/` sets the `logout` flag, which makes `login` revoke the tokens and delete the session.
- `check` is the permission endpoint other Turna instances call with [`iam_check`](/reference/server/http/middlewares/iam_check). The `host: auth` rule matches the internal service name, so the endpoint is not published on the public host.
- `cookie_name`, `session_key` and the Redis `key_prefix` are the contract with the other applications: every Turna that should see the same login uses the same three values.

::: tip Providers from the auth UI
Instead of the static `provider` map, the session can read its providers from the auth UI (*Federation → Session providers*) with `provider_source.auth_middleware: auth`. Changes then apply on the next request without a restart. See [`provider_source`](/reference/server/http/middlewares/session#dynamic-providers-from-the-auth-ui-provider-source).
:::

## First steps

1. Open `https://apps.example.com/auth/ui/`. With no `admin.permission` set, access is bootstrap-open.
2. Create a user, a role, and the OAuth client `platform-ui` used by the session provider.
3. Set the `admin` runtime namespace to lock down the management UI:

```sh
curl -X PUT https://apps.example.com/auth/v1/settings/admin \
  -d '{"value":{"permission":"turna.auth.admin","allow_missing_x_user":false}}'
```

For a single local instance, a `file` session store and a plain `dsn` are enough; Redis is needed as soon as more than one Turna reads the session. See the [`auth` reference](/reference/server/http/middlewares/auth) for runtime settings, LDAP sync, API keys, device flow, and SAML.
