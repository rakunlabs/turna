# oauth2 with keycloak

This example shows how to use oauth2 with keycloak.

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      template:
        template:
          headers:
            Content-Type: "text/html; charset=utf-8"
          template: |
            <html>
            <head>
              <title>Turna</title>
              <style>
                body {background-color: #f7fff7;}
                h1 {border-bottom: 2px solid #ff6b6b;}
                .logout {float: right; color: #ff6b6b; text-decoration: none;}
                pre {background-color: #faf0ca; overflow: auto; white-space: pre-wrap; word-wrap: break-word; }
              </style>
            </head>
            <body>
              <h1>Turna - Token Viewer <a class="logout" href="/logout">Logout</a></h1>
              <div>
                <p>Readed session from cookie</p>
                <pre><code>{{ .body | toPrettyJson }}</code></pre>
                <p>Access Token<p>
                <pre><code>{{ .body.access_token | crypto.JwtParseUnverified | toPrettyJson }}</code></pre>
              </div>
            </body>
            </html>
      auth:
        auth:
          provider:
            keycloak:
              base_url: "https://keycloak.example.com/auth" # /auth depends of your keycloak
              realm: "master"
              client_id: "ui"
              scopes:
                - openid
          redirect:
            cookie_name: "auth_keycloak"
            path: "/"
            logout:
              url: "/logout"
              redirect: "http://localhost:8080"
            schema: "http"
            session_key: "1234"
            session_store_name: "auth_keycloak"
            use_session: true
            # secure: true # enable if using https
            check_agent: true
            refresh_token: true
            redirect_match:
              enabled: true
      info:
        info:
          cookie: "auth_keycloak"
          session: true
          session_store_name: "auth_keycloak"
          base64: true
    routers:
      consul:
        path: /*
        middlewares:
          - auth
          - template
          - info
```
