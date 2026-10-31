# retry

`retry` sends a request again when the upstream fails: it cannot be reached, it times out, or it answers `502`, `503` or `504`. Waits between attempts grow exponentially with jitter. Failed attempts are dropped, so the client only gets the last response.

```yaml
server:
  http:
    middlewares:
      backend_retry:
        retry:
          attempts: 3
          initial_interval: 100ms
          max_interval: 2s
          respect_retry_after: true
      backend:
        service:
          loadbalancer:
            servers:
              - url: http://app-1:3000
              - url: http://app-2:3000
    routers:
      app:
        path: /*
        middlewares:
          - backend_retry
          - backend
```

| Field | Default | Description |
| --- | --- | --- |
| `attempts` | `2` | Retries after the first request. `0` disables retries. |
| `statuses` | `502, 503, 504` | Retried statuses, e.g. `[500-599]`. Connection errors and timeouts are always retried. |
| `methods` | `GET, HEAD, OPTIONS, TRACE, PUT, DELETE` | Retried methods. Requests with an `Idempotency-Key` header are retried for any method. |
| `initial_interval` | `100ms` | Wait before the first retry. It doubles for each retry. |
| `max_interval` | `2s` | Longest wait between attempts. |
| `respect_retry_after` | `false` | Wait as long as the `Retry-After` header of a failed response asks, up to `max_interval`. |
| `max_body_size` | `1MB` | Largest request body buffered to be sent again. Requests with larger bodies or bodies of unknown length are not retried. |
| `attempt_header` | | Request header with the attempt number starting at `1`, e.g. `X-Retry-Attempt`. |

## Methods

`POST` and `PATCH` are not retried by default: when the upstream got the request but the response was lost, a retry would run it twice. Requests with an `Idempotency-Key` header are retried for any method, which is only safe when something after `retry` honours the key, such as the upstream itself or [`idempotency`](./idempotency):

```yaml
middlewares:
  - backend_retry
  - payments_idempotency   # idempotency
  - backend
```

The header name is always `Idempotency-Key`, also when `idempotency` uses another `header`.

## Order

```yaml
middlewares:
  - backend_breaker   # circuit_breaker: fail fast when the backend is down
  - backend_retry     # retry
  - backend_timeout   # timeout: bounds each attempt
  - backend           # service
```

Put `timeout` after `retry` to bound each attempt, or before it to bound all attempts together. With [`service`](./service), each attempt is load balanced again, so a retry usually reaches another server.

WebSocket upgrades are not retried.
