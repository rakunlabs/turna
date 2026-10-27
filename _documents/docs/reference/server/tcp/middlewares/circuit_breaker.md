# circuit_breaker

`circuit_breaker` closes new connections for a while when the rest of the chain keeps failing, for example when `redirect` or `load_balancer` cannot dial the upstream. Clients fail fast instead of waiting for dial timeouts.

```yaml
server:
  tcp:
    middlewares:
      db_breaker:
        circuit_breaker:
          failure_ratio: 0.5
          min_requests: 10
          window: 10s
          open_duration: 30s
      db:
        load_balancer:
          servers:
            - address: 10.0.0.1:5432
    routers:
      postgres:
        entrypoints: [postgres]
        middlewares: [db_breaker, db]
```

| Field | Default | Description |
| --- | --- | --- |
| `failure_ratio` | `0.5` | Opens the circuit when failed / total connections in `window` reaches it. |
| `min_requests` | `10` | Connections needed in `window` before the ratio is checked. |
| `window` | `10s` | Rolling window of counted connections. |
| `open_duration` | `30s` | How long connections are rejected after the circuit opens. |
| `half_open_requests` | `1` | Probe connections let through after `open_duration`. All must succeed to close the circuit. |

A connection fails when the rest of the chain returns an error, such as a dial error. Rejections of later filters (`rate_limit`, `ip_deny_list`...) are not counted. Rejected connections are closed and logged at debug level.

Place `circuit_breaker` right before the terminal middleware. The HTTP [`circuit_breaker`](../../http/middlewares/circuit_breaker) works the same way on responses.
