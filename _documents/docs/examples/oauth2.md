# OAuth2 With IAM

This example shows the shape of an IAM-backed OAuth2/OIDC issuer. Replace the RSA keys and client/provider values before using it.

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      iam:
        iam:
          prefix_path: /iam
          database:
            path: ./data/iam
            flatten: true

      oauth2:
        oauth2:
          prefix_path: /oauth2
          iam_middleware: iam
          token:
            kid: turna-local
            token_lifetime: 15m
            refresh_lifetime: 24h
            cert:
              rsa:
                private_key: |
                  -----BEGIN RSA PRIVATE KEY-----
                  replace-with-private-key
                  -----END RSA PRIVATE KEY-----
                public_key: |
                  -----BEGIN PUBLIC KEY-----
                  replace-with-public-key
                  -----END PUBLIC KEY-----
          access_clients:
            local-ui:
              client_secret: local-secret
              scope: [openid]
              whitelist_urls:
                - http://localhost:3000/callback
          providers:
            keycloak:
              client_id: local-ui
              client_secret: local-secret
              scopes: [openid]
              cert_url: http://localhost:8081/realms/master/protocol/openid-connect/certs
              auth_url: http://localhost:8081/realms/master/protocol/openid-connect/auth
              token_url: http://localhost:8081/realms/master/protocol/openid-connect/token
          code:
            schema: http
            path: /oauth2/code
          store:
            active: ""

    routers:
      iam:
        path: /iam/*
        middlewares:
          - iam
      oauth2:
        path: /oauth2/*
        middlewares:
          - oauth2
```

Useful endpoints from this example:

| Endpoint | Description |
| --- | --- |
| `/oauth2/auth/keycloak` | Starts upstream code flow. |
| `/oauth2/code/keycloak` | Receives upstream callback. |
| `/oauth2/token` | Token endpoint. |
| `/oauth2/certs` | JWKS endpoint. |
| `/oauth2/userinfo` | Userinfo endpoint. |
