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
| `auth` | PostgreSQL-backed unified IAM/OAuth2 middleware. |
| `basic_auth` | HTTP Basic authentication with htpasswd hashes. |
| `block` | Block methods or paths. |
| `body_limit` | Reject request bodies larger than a size. |
| `cache` | Cache upstream responses in memory or Redis following `Cache-Control`. |
| `canary` | Split traffic between middleware chains by weight, sticky per client. |
| `circuit_breaker` | Stop calling a failing upstream for a while and answer `503`. |
| `compress` | Compress responses with zstd, Brotli or gzip. |
| `concurrency_limit` | Limit requests handled at the same time, with an optional queue. |
| `cors` | CORS headers and preflight handling. |
| `csrf` | Reject cross-site requests; optional double-submit token. |
| `decompress` | Decompress gzip, Brotli or zstd request bodies. |
| `dns_path` | Route to DNS-resolved instances selected from the path. |
| `fault_injection` | Delay or fail a share of requests for resilience testing. |
| `file_manager` | Browse, edit, upload and move files in a directory through a UI; serve them by path. |
| `folder` | Serve files and SPA assets from a directory. |
| `forward` | Forward proxy for HTTP and CONNECT requests. |
| `gateway` | Route by path, method, header and query with Gateway API style filters and weighted backends. |
| `gcp_token` | Google Cloud token via X.509 workload identity federation (OAuth2 endpoint or header inject). |
| `grpcui` | Browser UI for gRPC services. |
| `gzip` | Deprecated, use `compress`. |
| `headers` | Set, append or delete request/response headers. |
| `hello` | Return a static or templated response. |
| `iam_check` | Check request authorization through an IAM API. |
| `iam_forward_auth` | Forward-auth endpoint for external proxies. |
| `info` | Return cookie or session value content. |
| `inject` | Rewrite response bodies by path. |
| `ip_filter` | Allow or deny requests by client IP or CIDR. |
| `jwt` | Validate bearer JWTs with a JWKS URL, public keys or a secret. |
| `log` | Lightweight request log line. |
| `login` | Login UI and OAuth2 code/password flows backed by `session`. |
| `maintenance` | Answer with a maintenance response, switched by config or a file. |
| `oauth2_resource` | Protect an upstream (e.g. an MCP server) as an OAuth2 resource server (RFC 9728). |
| `path` | Replace the request path and optionally set request headers. |
| `print` | Print POST bodies to stderr for debugging. |
| `rate_limit` | Limit requests by IP, header, path and other keys, in memory or shared through Redis. |
| `real_ip` | Use the client IP sent by trusted proxies as the request address. |
| `redirect_continue` | Redirect when a regex rewrite changes the URL, otherwise continue. |
| `redirection` | Always redirect. |
| `regex_path` | Rewrite the URL path with a regex. |
| `request` | Make an outbound HTTP request and return its response. |
| `request_id` | Ensure `X-Request-Id` exists. |
| `role` | Role authorization (global and per path/method) and role based data after `session`. |
| `scope` | Check scopes in parsed claims. |
| `secure_headers` | HSTS, CSP with nonces and other security headers with safe defaults. |
| `service` | Reverse proxy and load balancer. |
| `session` | Validate bearer/session tokens and set identity headers. |
| `session_info` | Return selected claims from a session token. |
| `set` | Set Turna request-context values for other middlewares. |
| `splitter` | Select a sub-chain using expressions. |
| `strip_prefix` | Remove one of several path prefixes. |
| `telemetry` | OpenTelemetry traces and metrics of requests. |
| `template` | Render a response body template. |
| `timeout` | Bound request time with `504`, per path prefix. |
| `token_pass` | Generate a JWT and redirect or call another service. |
| `try` | Retry a chain with a rewritten path for selected response statuses. |
| `url` | Modify scheme, host, path, query, fragment, and port by rule. |
| `view` | Serve a combined Swagger/API documentation UI. |
| `webhook_signature` | Verify HMAC signatures of webhooks (GitHub, Stripe, Slack, Shopify, Standard Webhooks). |
