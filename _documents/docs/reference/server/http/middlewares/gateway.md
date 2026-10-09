# gateway

`gateway` dispatches requests to middleware chains with the matching rules of the Kubernetes Gateway API `HTTPRoute`. The Kubernetes provider generates it, but it can also be used directly.

```yaml
server:
  http:
    middlewares:
      api_v1:
        service:
          loadbalancer:
            servers:
              - url: http://v1:3000
      api_v2:
        service:
          loadbalancer:
            servers:
              - url: http://v2:3000
      routes:
        gateway:
          rules:
            - matches:
                - path: { type: prefix, value: /api }
                  headers:
                    - { name: X-Canary, value: "true" }
              middlewares: [api_v2]
            - matches:
                - path: { type: prefix, value: /api }
              filters:
                - request_headers:
                    set: { X-Gateway: turna }
              backends:
                - { weight: 90, middlewares: [api_v1] }
                - { weight: 10, middlewares: [api_v2] }
            - matches:
                - path: { type: exact, value: /old }
              filters:
                - redirect: { path: { replace_full_path: /new }, status_code: 301 }
    routers:
      main:
        path: [/*]
        middlewares: [routes]
```

## Matching

A rule matches when any of its `matches` match; an empty list matches every request. Within a match, all conditions must hold.

| Field | Description |
| --- | --- |
| `path.type` | `prefix` (default, matches whole path segments: `/api` matches `/api/x` but not `/apix`), `exact` or `regex`. |
| `path.value` | Path value, default `/`. |
| `method` | HTTP method. |
| `headers[]` | `name`, `value` and `type` (`exact` or `regex`). |
| `query[]` | `name`, `value` and `type` (`exact` or `regex`). |

When several rules match, the most specific wins, in this order: higher `priority`, exact path, longest prefix, method match, more header matches, more query matches, then rule order. Requests that match no rule get `404`.

## Rule

| Field | Description |
| --- | --- |
| `filters[]` | Run in order before the middlewares. |
| `middlewares` | Named middlewares, usually a `service`. |
| `backends[]` | Weighted middleware chains; one is picked per request. `weight: 0` disables a backend. `status` answers with a fixed status instead. |
| `timeout` | Request timeout. When it expires before a response starts, the response is `504`. |
| `status` | Status when the chain does not answer, default `500`. |

## Filters

| Filter | Description |
| --- | --- |
| `request_headers` | `set`, `add` and `remove` request headers. |
| `response_headers` | `set`, `add` and `remove` response headers. |
| `rewrite` | `hostname` and `path` (`replace_full_path` or `replace_prefix_match`). |
| `redirect` | `scheme`, `hostname`, `port`, `path` and `status_code` (default `302`). |
| `mirror` | Same options as the `service` mirror. |
| `middlewares` | Run named middlewares at this position. |

## gRPC

Rules in `grpc` match requests with content type `application/grpc` instead of `rules`. A `grpc` match selects the service and method, and can be combined with `headers`:

```yaml
routes:
  gateway:
    grpc:
      - matches:
          - grpc: { service: pkg.Users, method: Get }
            headers:
              - { name: version, value: two }
        middlewares: [users_v2]
      - matches:
          - grpc: { service: pkg.Users }
        middlewares: [users]
```

`grpc.type` is `exact` (default) or `regex`. More specific matches win: longer service, longer method, more headers. Errors of the gateway are returned to gRPC clients as gRPC status codes (`Unimplemented` when nothing matches, `Unavailable` when the upstream is unreachable). Use a `service` with `h2c: true` for plain text gRPC upstreams.

Set `passthrough: true` to call the next middleware of the router after a rule's chain, or when no rule matches, instead of answering.
