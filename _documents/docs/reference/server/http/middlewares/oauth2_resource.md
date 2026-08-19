# oauth2_resource

`oauth2_resource` protects an upstream service — typically an **MCP server** — as an OAuth2 resource server. It pairs with the [`auth`](./auth) middleware acting as the authorization server:

- Serves the **RFC 9728 protected resource metadata** document under `/.well-known/oauth-protected-resource[...]` so discovery-driven clients (MCP spec) find the authorization server.
- Validates **bearer access tokens in-process** against the auth middleware's signing key (no HTTP self-calls, via the issuer registry).
- Answers unauthenticated requests with `401` and a `WWW-Authenticate: Bearer resource_metadata="..."` challenge.
- Optionally enforces the **RFC 8707 audience binding** (`aud` must contain the resource identifier) and **required scopes** (`403 insufficient_scope`).
- Consults the auth middleware's **revocation list** (RFC 7009 denylist) on every request.
- Strips and re-sets the identity header (`X-User`) from the validated token for the upstream.

```yaml
server:
  http:
    middlewares:
      auth:
        auth:
          database:
            dsn: postgres://turna:turna@localhost:5432/turna?sslmode=disable
          encryption:
            key: ${AUTH_ENCRYPTION_KEY}
      mcp-guard:
        oauth2_resource:
          auth_middleware: "auth"
          resource: "https://example.com/mcp"
          authorization_servers:
            - "https://example.com/auth/oauth2"
          scopes_supported: ["openid", "mcp"]
          required_scopes: ["mcp"]
      mcp-upstream:
        service:
          loadbalancer:
            servers:
              - url: "http://mcp-server:8080"
    routers:
      mcp:
        path: /mcp/*
        middlewares:
          - mcp-guard
          - mcp-upstream
      # RFC 9728 metadata for the resource
      mcp-wellknown:
        path: /.well-known/oauth-protected-resource*
        middlewares:
          - mcp-guard
      # RFC 8414 discovery for the authorization server
      auth-wellknown:
        path: /.well-known/oauth-authorization-server*
        middlewares:
          - auth
      auth:
        path: /auth/*
        middlewares:
          - session
          - auth
```

## Fields

| Field | Description |
| --- | --- |
| `auth_middleware` | Name of the `auth` middleware whose tokens are accepted. Required; validation runs in-process through the issuer registry. |
| `resource` | Canonical RFC 8707/9728 resource identifier (e.g. `https://example.com/mcp`). Empty derives `{scheme}://{host}` from each request. |
| `authorization_servers` | Issuer URLs listed in the metadata. Empty derives `{scheme}://{host}{auth_prefix_path}/oauth2` per request. |
| `auth_prefix_path` | Prefix of the auth middleware, only used to derive the issuer when `authorization_servers` is empty. Default `/auth`. |
| `scopes_supported` | Scopes advertised in the metadata document. |
| `required_scopes` | Scopes that must all be present in the token; missing ones answer `403 insufficient_scope`. |
| `check_audience` | Require the token `aud` to contain the resource identifier. Default `true` when `resource` is set. |
| `check_revocation` | Consult the issuer's revocation denylist per request (one database lookup). Default `true`. |
| `user_header` | Header receiving the authenticated user alias. Default `X-User`. |
| `scope_header` | Optional header receiving the granted scopes. |

## Behavior

Requests with a path under `/.well-known/oauth-protected-resource` answer the metadata document:

```json
{
  "resource": "https://example.com/mcp",
  "authorization_servers": ["https://example.com/auth/oauth2"],
  "bearer_methods_supported": ["header"],
  "scopes_supported": ["openid", "mcp"]
}
```

Every other request must carry `Authorization: Bearer <access token>`:

| Case | Response |
| --- | --- |
| Missing/invalid/expired token, refresh token, revoked `jti`, wrong audience | `401` + `WWW-Authenticate: Bearer resource_metadata="...", error="invalid_token"` |
| Missing required scope | `403` + `WWW-Authenticate: ... error="insufficient_scope"` |
| Valid token | Upstream is called with `user_header` set from `preferred_username` (fallback `sub`); incoming values of that header are always stripped first. |

## MCP flow

With the [`auth`](./auth) middleware's `registration` setting enabled, an MCP client needs zero manual setup:

1. Client calls the MCP endpoint, gets `401` with the resource metadata URL.
2. Metadata points at the authorization server; the client fetches RFC 8414 discovery and registers itself at `/auth/oauth2/register` (public client, PKCE).
3. The user approves the request on `/auth/oauth2/authorize` → consent (session login).
4. The client redeems the code (with `resource=https://example.com/mcp`) and calls the MCP endpoint with the bearer token; the audience check ties the token to this resource.
