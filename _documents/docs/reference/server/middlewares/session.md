# session

Record token, validate it and redirect to login page.

```yaml
middlewares:
  session: # custom name
    session: # middleware name
      session_key: "" # secret key for storage
      store:
        active: "" # redis or file
        redis:
          address: "" # localhost:6379
          username: ""
          password: ""
          tls:
            enabled: false
            cert_file: ""
            key_file: ""
            ca_file: ""
        file:
          path: "" # if empty then it will create tempdir
      options: # cookie options for store session key
        path: "/"
        max_age: 86400 # seconds to store cookie
        domain: ""
        secure: false
        http_only: false
        same_site: 0 # // SameSite for Lax 2, Strict 3, None 4
      cookie_name: "" # set cookie's name
      action:
        active: token # token
        token:
          login_path: "/login/" # for redirection path
          disable_refresh: false # disable refresh token
          insecure_skip_verify: false # token requests
      provider:
        my_provider: # custom name
          password_flow: false # enable password flow, for ui get request
          priority: 0 # priority for the provider, for ui get request
          oauth2:
            token_url: ""
            cert_url: ""
            client_id: ""
            client_secret: ""
            scopes: []
            introspect_url: ""
            logout_url: ""
```

> Put this above of the your middleware to make it token protection.

## Extra

For example, to disable redirection on `/whoami/*` paths we need to set a middleware before session to change the behaviour.

session middleware has 2 options

```
token_header -> Add Authorization Bearer header
token_header_delete -> Delete Authorization Bearer header, useful for token logins
disable_redirect -> disable redirection to login page, it will return 407 error
```

```yaml
server:
  http:
    middlewares:
      token:
        set:
          values:
          - token_header
          - disable_redirect
    routers:
      whoami:
        path: /whoami/*
        middlewares:
          - token
          - session
          - whoami
      main:
        path: /*
        middlewares:
          - session
          - main
```

> Check login example.
