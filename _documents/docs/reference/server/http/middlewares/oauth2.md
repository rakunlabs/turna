# oauth2

`oauth2` exposes OAuth2/OIDC-compatible endpoints backed by a registered [`iam`](./iam) middleware. It can redirect users to external providers, mint Turna-signed RS256 tokens, serve JWKS, and return userinfo.

> Deprecated: use [`auth`](./auth) for new PostgreSQL-backed IAM/OAuth2 setups. `oauth2` remains available for existing `iam` deployments during migration.

```yaml
server:
  http:
    middlewares:
      iam:
        iam:
          prefix_path: /iam
          database:
            path: ./data/iam
      oauth2:
        oauth2:
          prefix_path: /oauth2
          iam_middleware: iam
          token:
            kid: turna
            token_lifetime: 15m
            refresh_lifetime: 24h
            cert:
              rsa:
                private_key: |
                  -----BEGIN RSA PRIVATE KEY-----
                  ...
                  -----END RSA PRIVATE KEY-----
                public_key: |
                  -----BEGIN PUBLIC KEY-----
                  ...
                  -----END PUBLIC KEY-----
          access_clients:
            app:
              client_secret: secret
              scope: [openid]
              whitelist_urls:
                - http://localhost:3000/callback
          providers:
            keycloak:
              client_id: app
              client_secret: secret
              scopes: [openid]
              auth_url: http://idp/auth
              token_url: http://idp/token
              cert_url: http://idp/certs
          code:
            schema: http
            path: /oauth2/code
          store:
            active: ""
```

## Fields

| Field | Description |
| --- | --- |
| `prefix_path` | Base path for OAuth2/OIDC endpoints. |
| `iam_middleware` | Required registered `iam` middleware name. |
| `token.kid` | Key ID advertised in JWKS and tokens. |
| `token.cert.rsa.private_key` | PEM RSA private key for signing. |
| `token.cert.rsa.private_key_base64` | Base64 encoded private key alternative. |
| `token.cert.rsa.public_key` | PEM RSA public key for JWKS. |
| `token.cert.rsa.public_key_base64` | Base64 encoded public key alternative. |
| `token.token_lifetime` | Access token lifetime. Default is `15m`. |
| `token.refresh_lifetime` | Refresh token lifetime. Default is `24h`. |
| `access_clients` | OAuth clients accepted by the token endpoint. |
| `providers` | Upstream OAuth2 providers used by auth/code flows. |
| `code` | Redirect URL construction and upstream TLS options. |
| `store` | Temporary code/state store. Empty means memory; `active: redis` uses Redis. |
| `pass_lower` | Lowercase password-flow password before checking. |
| `well_known` | Custom OpenID configuration responses by name. |
| `custom_info` | Custom userinfo claim templates by name. |

## Endpoints

For `prefix_path: /oauth2`, the middleware serves:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/oauth2/auth/{provider}` | Start authorization code flow against an upstream provider. |
| `GET` | `/oauth2/code/{provider}` | Receive provider callback and create a Turna auth code. |
| `POST` | `/oauth2/token` | Token endpoint. |
| `GET` | `/oauth2/certs` | JWKS endpoint. |
| `GET` | `/oauth2/openid/{custom}/.well-known/openid-configuration` | Custom well-known response. |
| `GET` | `/oauth2/userinfo` | Userinfo for the access token. |
| `GET` | `/oauth2/userinfo/{custom}` | Userinfo with custom claim templates. |
