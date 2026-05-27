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
| `GET` | `/login/auth/code/{provider}` | Start or finish OAuth2 authorization code flow. |
| `POST` | `/login/auth/token/{provider}` | Password flow token login. |
| `GET` | `/login/auth/info/ui` | Provider list for the UI. |
| `GET` | `/login/auth/status` | Login status endpoint. |

## Logout

Set the `logout` context value before `login` to delete the session and redirect through the login middleware.

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
