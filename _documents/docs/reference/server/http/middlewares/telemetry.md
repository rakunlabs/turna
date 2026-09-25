# telemetry

`telemetry` records OpenTelemetry traces and metrics of HTTP requests with [`github.com/rakunlabs/ada/middleware/telemetry`](https://github.com/rakunlabs/ada). It uses the global providers configured in the top level [`telemetry`](../../../config#telemetry) section; without a collector it is a noop.

```yaml
telemetry:
  collector: otel-collector:4317

server:
  http:
    middlewares:
      otel:
        telemetry:
          skip_paths:
            - /healthz
            - /metrics
          trusted_proxies:
            - 10.0.0.0/8
    routers:
      app:
        path: /
        middlewares:
          - otel
          - backend
```

| Field | Default | Description |
| --- | --- | --- |
| `public_endpoint` | `false` | Start a new trace for each request and link the incoming trace instead of continuing it. Use it for endpoints exposed to the internet. |
| `skip_paths` | | Path prefixes that are not instrumented. |
| `trusted_proxies` | | CIDRs whose `X-Forwarded-For` / `X-Real-IP` headers are used for `client.address`. Without it the peer address is used. |

Traces follow the W3C trace context propagated in request headers. Metrics follow the OpenTelemetry HTTP server semantic conventions (`http.server.request.duration`, `http.server.active_requests`, request/response body sizes) plus `http.server.total_requests`. Place it first in the router to measure the whole chain.
