# circuit_breaker

`circuit_breaker` stops sending requests to a failing upstream for a while. When too many requests fail, the circuit opens and requests are answered with `503 Service Unavailable` without calling the next middleware.

```yaml
server:
  http:
    middlewares:
      backend_breaker:
        circuit_breaker:
          failure_ratio: 0.5
          min_requests: 20
          window: 10s
          open_duration: 30s
          slow_threshold: 5s
      backend:
        service:
          loadbalancer:
            servers:
              - url: http://localhost:3000
    routers:
      app:
        path: /*
        middlewares:
          - backend_breaker
          - backend
```

| Field | Default | Description |
| --- | --- | --- |
| `failure_ratio` | `0.5` | Opens the circuit when failed / total requests in `window` reaches it, between `0` and `1`. |
| `min_requests` | `10` | Requests needed in `window` before the ratio is checked. |
| `window` | `10s` | Rolling window of counted requests. |
| `open_duration` | `30s` | How long requests are rejected after the circuit opens. |
| `half_open_requests` | `1` | Probe requests let through after `open_duration`. |
| `failure_statuses` | `502, 503, 504` | Response statuses counted as failures. |
| `slow_threshold` | | Responses slower than this count as failures. `0` disables it. |
| `status_code` | `503` | Status of rejected requests. |

## States

- **Closed:** requests pass and their results are counted.
- **Open:** requests are rejected with `Retry-After` set to the time left in `open_duration`.
- **Half open:** after `open_duration`, `half_open_requests` probes are let through. If all succeed the circuit closes; if one fails it opens again.

An unreachable upstream or a timeout of `service` or `gateway` is always a failure, whatever its status. Requests canceled by the client are not counted. gRPC requests get an `UNAVAILABLE` status instead of `503`.

The state is kept in memory per Turna instance and per named middleware. Use one `circuit_breaker` for each upstream so one failing service does not block others.

Put `circuit_breaker` before `service`. With `retry` in `service`, the breaker sees only the final result of the retries.
