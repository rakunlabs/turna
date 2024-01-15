# login

This middleware is used to give a login screen to the user.

Token is stored in the session and support `redis` and `file` storage.

```yaml
middlewares:
  login: # custom name
    login: # middleware name
      info: # optional info to show in the login page
        title: "Turna Login"
      session_middleware: "session" # session middleware name to use record token
      ui:
        embed_path_prefix: "/login/" # this known as base path
        external_folder: false # if true, get requests will be forwarded to the next middleware
      default_provider: keycloak # default provider to use (custom name)
      provider: # privder for multiple login options
        keycloak: # custom provider name
          oauth2:
            client_id: "test"
            client_secret: ""
            cert_url: "http://localhost:8080/realms/master/protocol/openid-connect/certs"
            token_url: "http://localhost:8080/realms/master/protocol/openid-connect/token"
      request:
        insecure_skip_verify: false # if true, skip tls verification for token request
```

> Need to use with session middleware.

`redirect_path` query param using to redirect after login.

If you are already login then it will redirect back to `redirect_path` or root of the page.

## Extra

login middleware has option for logout, to enable this set `logout` ctx value.

When you go to `/logout/` entrypoint than, it will erase your cookie and redirect to login page.

```yaml
server:
  http:
    middlewares:
      logout:
        set:
          values:
          - logout
    routers:
      login:
        path: /login/*
        middlewares:
          - login
      logout:
        path: /logout/*
        middlewares:
          - logout
          - login
```
