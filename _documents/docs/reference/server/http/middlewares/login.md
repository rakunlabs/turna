# login

This middleware is used to give a login screen to the user.

Token is stored in the session and support `redis` and `file` storage.

```yaml
middlewares:
  login: # custom name
    login: # middleware name
      path:
        base: "" # base path of the login page
        base_url: "" # for adding prefix like https://example.com
        code: "" # code api path
        token: "" # token api path
        info_ui: "" # info ui path
      redirect:
        base_url: ""
        schema: "https" # schema to use for redirect, default is https
      ui:
        external_folder: false # if true, get requests will be forwarded to the next middleware
      info: # optional info to show in the login page
        title: "Turna Login"
      session_middleware: "session" # session middleware name to use record token
      state_cookie: # cookie to store state of code flow login
        cookie_name: "auth_state"
        max_age: 360
        path: "/"
        domain: ""
        secure: false
        same_site: 2 # SameSite for Lax 2, Strict 3, None 4
        http_only: false
      success_cookie: # cookie to store success message of code flow login
        cookie_name: "auth_verify"
        max_age: 60
        path: "/"
        domain: ""
        secure: false
        same_site: 2 # SameSite for Lax 2, Strict 3, None 4
      request:
        insecure_skip_verify: false # if true, skip tls verification for token request
```

> Need to use with session middleware.

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
