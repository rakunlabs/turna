# jwt

`jwt` validates bearer JWTs without a session or login flow. Keys come from the JWKS URL of an identity provider, from static public keys or from a shared secret. Use [`session`](./session) or [`oauth2_resource`](./oauth2_resource) for full OAuth2 flows.

```yaml
server:
  http:
    middlewares:
      api_jwt:
        jwt:
          jwks_url: https://idp.example.com/.well-known/jwks.json
          issuer:
            - https://idp.example.com
          audience:
            - my-api
          required_claims:
            scope: api.read
          claim_headers:
            X-User-Id: sub
            X-User-Email: email
          strip_token: true
```

| Field | Default | Description |
| --- | --- | --- |
| `jwks_url` | | JWK Set of the issuer. |
| `refresh_interval` | `1h` | How often the JWK Set is fetched again. |
| `public_keys` | | PEM public keys or certificates, inline or as file paths. |
| `secret` | | Shared secret for `HS256`, `HS384` and `HS512`. |
| `algorithms` | from keys | Accepted algorithms. Defaults to the RSA, ECDSA and EdDSA ones with keys, plus `HS*` with `secret`. |
| `issuer` | | Accepted `iss` values. |
| `audience` | | Accepted `aud` values. The token must contain one of them. |
| `leeway` | `30s` | Clock skew allowed for `exp`, `nbf` and `iat`. |
| `required_claims` | | Claims that must be present. A value must match; for a list or a space separated string (`scope`) it must be one of the items. |
| `token_source` | `header:Authorization` | Where the token is read: `header:<name>`, `query:<name>` or `cookie:<name>`. The first one found is used. |
| `optional` | `false` | Let requests without a token through. Invalid tokens are still rejected. |
| `claim_headers` | | Request headers set from claims. Lists are joined with commas. |
| `strip_token` | `false` | Remove the token from the request before passing it on. |
| `roles_claim` | | Extra claims holding roles for [`role`](./role), see below. |

At least one of `jwks_url`, `public_keys` or `secret` is required. Tokens must have `exp`. The `none` algorithm is never accepted.

## Keys

The JWK Set is fetched on the first request, so Turna starts even when the issuer is down. A token with an unknown `kid` fetches the set again, at most once a minute, to follow key rotation. Tokens without `kid` are checked against `public_keys`.

## Responses

Missing or invalid tokens get `401 Unauthorized` with a `WWW-Authenticate: Bearer` header.

The headers in `claim_headers` are removed from every request first, so clients cannot set them themselves.

## Roles and scopes

A valid token's claims are stored like [`session`](./session) stores them, so [`role`](./role) and [`scope`](./scope) can follow `jwt` in the chain:

```yaml
server:
  http:
    middlewares:
      api_jwt:
        jwt:
          jwks_url: https://idp.example.com/.well-known/jwks.json
      admin_only:
        role:
          roles:
            - admin
      can_write:
        scope:
          scopes:
            - api.write
    routers:
      admin:
        path: /admin/*
        middlewares:
          - api_jwt
          - admin_only
          - can_write
          - backend
```

Roles are read from `roles`, `realm_access.roles` and `resource_access.<client>.roles` (the Keycloak layout). Scopes are read from the space separated `scope` claim.

For identity providers that keep roles elsewhere, list the claims in `roles_claim`. They are read in addition to the default ones:

```yaml
jwt:
  jwks_url: https://idp.example.com/.well-known/jwks.json
  roles_claim:
    - groups                      # Azure AD, Okta
    - https://example.com/roles   # Auth0 namespaced claim
    - app.permissions             # nested object
```

A name is looked up as a top-level claim first, so names with dots such as URLs work. When there is no such claim, it is read as a dot separated path into nested objects. The claim can be a list or a string separated by spaces or commas.
