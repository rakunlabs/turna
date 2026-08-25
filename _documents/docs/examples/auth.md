# Auth

This example runs the PostgreSQL-backed [`auth`](/reference/server/http/middlewares/auth) middleware as a complete identity provider, with [`session`](/reference/server/http/middlewares/session) and [`login`](/reference/server/http/middlewares/login) in front for browser authentication.

Requirements: a reachable PostgreSQL database (migrations run automatically) and an encryption key.

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
            dsn: postgres://turna:turna@localhost:5432/turna?sslmode=disable
          encryption:
            key: ${AUTH_ENCRYPTION_KEY}

      session:
        session:
          store:
            active: file
            file:
              session_key: my_secret_key
          provider:
            turna:
              auth_middleware: "auth" # in-process token validation and refresh
              password_flow: true
              passkey: true
              oauth2:
                client_id: "ui" # OAuth client registered in auth
                scopes: [openid]
          action:
            token:
              login_path: /login/

      login:
        login:
          session_middleware: session
          path:
            base: /login/

      app:
        hello:
          message: hello authenticated user

    routers:
      login:
        path: /login/*
        middlewares:
          - login
      auth:
        path: /auth/*
        middlewares:
          - session # sets X-User for the auth API/UI
          - auth
      app:
        path: /*
        middlewares:
          - session
          - app
```

How the pieces fit:

- `auth` serves the IAM API, OAuth2/OIDC endpoints, and the embedded management UI under `/auth`. All runtime settings (token lifetimes, clients, providers, LDAP, TOTP, passkeys, ...) live in PostgreSQL and are managed through `/auth/ui/`.
- `session` references the auth middleware by name (`auth_middleware`), so JWT validation and token refresh happen in-process without HTTP self-calls.
- `login` renders the login page at `/login/` and authenticates users with the password grant and/or passkeys.

First steps after starting:

1. Open `http://localhost:8080/auth/ui/` — with no `admin.permission` set, access is bootstrap-open.
2. Create a user, a role, and an OAuth client (`ui`) for the session provider.
3. Set the `admin` runtime namespace to lock down the management UI:

```sh
curl -X PUT http://localhost:8080/auth/v1/settings/admin \
  -d '{"value":{"permission":"turna.auth.admin","allow_missing_x_user":false}}'
```

Use a Redis session store (`store.active: redis`) when running more than one instance. See the [`auth` reference](/reference/server/http/middlewares/auth) for runtime settings, LDAP sync, API keys, device flow, and SAML.
