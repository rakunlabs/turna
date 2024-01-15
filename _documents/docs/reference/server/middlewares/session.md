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
      options: # cookie options
        path: "/"
        max_age: 86400 # seconds to store cookie
        domain: ""
        secure: false
        http_only: false
        same_site: 0 # // SameSite for Lax 2, Strict 3, None 4
      cookie_name: "" # set cookie's name
      value_name: "" # leave empty, internal usage
      actions:
        active: token # token
        token:
          login_path: "/login/" # for redirection path
          disable_refresh: false # disable refresh token
          insecure_skip_verify: false # token requests
          oauth2:
            token_url: ""
            cert_url: ""
            client_id: ""
            client_secret: ""
      information: # reachable with login middleware's path + ./api/v1/info/token
        values: [] # tokens claims
        custom: {} # for custom values
        roles: false # add roles information []string
        scopes: false # add scopes information []string
```

> Put this above of the your middleware to make it token protection.

## Extra

For example, to disable redirection on `/whoami/*` paths we need to set a middleware before session to change the behaviour.

session middleware has 2 options

```
token_header -> Add Authorization Bearer header
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
