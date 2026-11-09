# MCP Authorization

Turna can be the OAuth authorization server **and** the resource server of a remote [Model Context Protocol](https://modelcontextprotocol.io) server, so MCP clients such as Claude, ChatGPT, Cursor, VS Code or opencode sign users in and call tools without any client configuration beyond the server URL.

It implements the [MCP authorization specification](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization) end to end:

| Step | Standard | turna |
| --- | --- | --- |
| Client calls `/mcp`, gets `401` with a `resource_metadata` pointer | RFC 9728 | [`oauth2_resource`](/reference/server/http/middlewares/oauth2_resource) |
| Client reads the protected resource metadata, finds the authorization server | RFC 9728 | `oauth2_resource` |
| Client reads the authorization server metadata | RFC 8414 | [`auth`](/reference/server/http/middlewares/auth) |
| Client registers itself, or presents a client metadata URL | RFC 7591, Client ID Metadata Documents | `auth` |
| User signs in and consents; client gets a code with PKCE | OAuth 2.1, RFC 7636 | `auth` + [`login`](/reference/server/http/middlewares/login) |
| Token is bound to the MCP server URL | RFC 8707 | `auth`, checked by `oauth2_resource` |
| Optional proof of possession | RFC 9449 (DPoP) | `auth` + `oauth2_resource` |
| MCP server receives the user | `X-User` / `X-Scope` headers | `oauth2_resource` |

The MCP server itself needs no OAuth code: it reads the user from `X-User`.

## Run the demo

[`ci/mcp-demo`](https://github.com/rakunlabs/turna/tree/main/ci/mcp-demo) puts turna in front of a small MCP server and connects with the official MCP TypeScript SDK client, which performs the whole discovery, registration and PKCE flow by itself:

```sh
ci/mcp-demo/run.sh
```

```text
authorize: http://localhost:8082/auth/oauth2/authorize
registered client: dcr-01m6ye2734q07skj4wpn06d6pr
tools: whoami, add
you are demo (scope: openid)
2 + 3 = 5
```

## Configuration

The relevant parts of [`ci/mcp-demo/turna.yml`](https://github.com/rakunlabs/turna/blob/main/ci/mcp-demo/turna.yml):

```yaml
server:
  http:
    middlewares:
      auth:
        auth:
          database:
            dsn: postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
          encryption:
            key: <base64 32 byte key>
      session:
        session:
          # the authorization server's public plane (token, register,
          # discovery, consent) stays reachable for MCP clients
          auth_skip_paths: [auth]
          provider:
            turna:
              auth_middleware: auth
              password_flow: true
              oauth2:
                client_id: ui
                client_secret: <secret of the "ui" client>
          action:
            token:
              login_path: /login/
      login:
        login:
          session_middleware: session
          path:
            base: /login/
      mcp-guard:
        oauth2_resource:
          auth_middleware: auth
          resource: https://example.com/mcp           # pin in production
          authorization_servers: [https://example.com/auth/oauth2]
          scopes_supported: [openid, mcp]
          required_scopes: [mcp]
          scope_header: X-Scope
      mcp-upstream:
        service:
          loadbalancer:
            servers:
              - url: http://127.0.0.1:9090
    routers:
      mcp:
        path: [/mcp, /mcp/*]
        middlewares: [mcp-guard, mcp-upstream]
      mcp-wellknown:
        path: [/.well-known/oauth-protected-resource, /.well-known/oauth-protected-resource/*]
        middlewares: [mcp-guard]
      auth-wellknown:
        path: /.well-known/*
        middlewares: [session, auth]
      auth:
        path: /auth/*
        middlewares: [session, auth]
      login:
        path: /login/*
        middlewares: [login]
```

Then enable dynamic client registration and point the consent page at the login page (runtime settings, through the admin UI or API):

```sh
curl -X PUT https://example.com/auth/v1/settings/registration \
  -d '{"value":{"enabled":true,"client_lifetime":"720h"}}'
curl -X PUT https://example.com/auth/v1/settings/authorize \
  -d '{"value":{"login_url":"/login/"}}'
```

::: tip Router paths
A trailing `*` only matches after a slash: `/mcp/*` does not match `/mcp`. List both, as above; MCP clients call the exact URL.
:::

## Hardening checklist

- Pin `resource` and `authorization_servers` on `oauth2_resource` and `oauth2.base_url` on `auth`; otherwise they follow the request host and forwarded headers.
- Keep `check_audience` (default on) so a token issued for another MCP server is refused (RFC 8707).
- Require a scope (`required_scopes`) so ordinary sign-in tokens cannot call tools.
- Limit dynamic registration with `registration.client_lifetime`; clients can also identify themselves with a client metadata URL instead of registering.
- Turn on [DPoP](/reference/server/http/middlewares/auth#oauth2) for clients that support it, and `require_dpop` on clients that must.
- Watch the [audit log](/reference/server/http/middlewares/auth#audit-log) for `code.reuse`, `refresh.reuse` and failed `login` events.

## Connecting real clients

Use the public URL of the MCP endpoint, for example `https://example.com/mcp`:

- **Claude** (claude.ai / Claude Desktop): *Settings → Connectors → Add custom connector*.
- **Claude Code**: `claude mcp add --transport http turna https://example.com/mcp`.
- **VS Code / Cursor / opencode**: add an HTTP MCP server with that URL; the editor opens the browser for sign-in.

Each client registers itself on first use and shows up under **OAuth server clients** in the admin UI (`dcr-…` ids), where it can be removed.
