# HTTP Middlewares

HTTP middleware instances are declared under `server.http.middlewares` and referenced by name from routers.

```yaml
server:
  http:
    middlewares:
      strip_api:
        strip_prefix:
          prefix: /api
      backend:
        service:
          loadbalancer:
            servers:
              - url: http://localhost:3000
    routers:
      app:
        path: /api/*
        middlewares:
          - strip_api
          - backend
```

## Important Rule

A single named middleware can hold only one middleware type. If you need both headers and a service proxy, define two named middlewares and chain them in the router.

## Supported Keys

| Key | Purpose |
| --- | --- |
| `access_log` | Structured request/response logging. |
| `add_prefix` | Add a path prefix before the next middleware. |
| `basic_auth` | HTTP Basic authentication with htpasswd hashes. |
| `block` | Block methods or paths. |
| `cors` | CORS headers and preflight handling. |
| `decompress` | Decompress gzip request bodies. |
| `dns_path` | Route to DNS-resolved instances selected from the path. |
| `folder` | Serve files and SPA assets from a directory. |
| `forward` | Forward proxy for HTTP and CONNECT requests. |
| `grpcui` | Browser UI for gRPC services. |
| `gzip` | Compress responses. |
| `headers` | Set or delete request/response headers. |
| `hello` | Return a static or templated response. |
| `iam` | Embedded IAM API/UI and permission store. |
| `iam_check` | Check request authorization through an IAM API. |
| `iam_forward_auth` | Forward-auth endpoint for external proxies. |
| `info` | Return cookie or session value content. |
| `inject` | Rewrite response bodies by path. |
| `log` | Lightweight request log line. |
| `login` | Login UI and OAuth2 code/password flows backed by `session`. |
| `oauth2` | OAuth2/OIDC-compatible endpoints backed by IAM. |
| `path` | Replace the request path and optionally set request headers. |
| `print` | Print POST bodies to stderr for debugging. |
| `rate_limit` | Limit requests by all traffic, IP, or real IP. |
| `redirect_continue` | Redirect when a regex rewrite changes the URL, otherwise continue. |
| `redirection` | Always redirect. |
| `regex_path` | Rewrite the URL path with a regex. |
| `request` | Make an outbound HTTP request and return its response. |
| `request_id` | Ensure `X-Request-Id` exists. |
| `role` | Check roles in parsed claims. |
| `role_check` | Path/method role authorization after `session`. |
| `role_data` | Return data based on roles in parsed claims. |
| `scope` | Check scopes in parsed claims. |
| `service` | Reverse proxy and load balancer. |
| `session` | Validate bearer/session tokens and set identity headers. |
| `session_info` | Return selected claims from a session token. |
| `set` | Set Turna request-context values for other middlewares. |
| `splitter` | Select a sub-chain using expressions. |
| `strip_prefix` | Remove one of several path prefixes. |
| `template` | Render a response body template. |
| `token_pass` | Generate a JWT and redirect or call another service. |
| `try` | Retry a chain with a rewritten path for selected response statuses. |
| `url` | Modify scheme, host, path, query, fragment, and port by rule. |
| `view` | Serve a combined Swagger/API documentation UI. |
