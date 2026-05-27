# Login

This example combines `session`, `login`, `session_info`, and `role_check` for a browser application backed by an external OAuth2 provider.

```yaml
server:
  entrypoints:
    web:
      address: ":8082"
  http:
    middlewares:
      session:
        session:
          cookie_name: turna_auth
          store:
            active: file
            file:
              session_key: my_secret_key
          options:
            http_only: true
            secure: false
            same_site: 2
          provider:
            keycloak:
              name: Keycloak
              password_flow: true
              oauth2:
                client_id: test
                client_secret: ""
                scopes: [openid]
                cert_url: http://localhost:8080/realms/master/protocol/openid-connect/certs
                token_url: http://localhost:8080/realms/master/protocol/openid-connect/token
                auth_url: http://localhost:8080/realms/master/protocol/openid-connect/auth
                logout_url: http://localhost:8080/realms/master/protocol/openid-connect/logout
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

      logout_flag:
        set:
          values:
            - logout

      token_api_mode:
        set:
          values:
            - token_header
            - disable_redirect

      current_user:
        session_info:
          session_middleware: session
          information:
            values:
              - preferred_username
              - email
            roles: true

      api_roles:
        role_check:
          allow_others: true
          path_map:
            - regex_path: ^/api/transaction/.*
              map:
                - roles: [transaction_r, transaction_rw]
                  methods: [GET]
                - roles: [transaction_rw]
                  write_methods: true

      app:
        hello:
          content_type: text/html; charset=utf-8
          message: |
            <html>
              <body>
                <h1>Turna protected page</h1>
                <a href="/logout/">Logout</a>
              </body>
            </html>

      api:
        service:
          loadbalancer:
            servers:
              - url: http://localhost:9090

    routers:
      login:
        path: /login/*
        middlewares:
          - login
      logout:
        path: /logout/*
        middlewares:
          - logout_flag
          - login
      current_user:
        path: /auth/info
        middlewares:
          - current_user
      api:
        path: /api/*
        middlewares:
          - token_api_mode
          - session
          - api_roles
          - api
      app:
        path: /*
        middlewares:
          - session
          - app
```

Routes under `/api/*` return `407` instead of redirecting because `token_api_mode` sets `disable_redirect`. Browser routes use the default redirect behavior and send users to `/login/`.
