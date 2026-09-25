# conn_limit

`conn_limit` limits concurrent connections, in total and per client IP. Extra connections are closed immediately.

```yaml
server:
  tcp:
    middlewares:
      limit:
        conn_limit:
          max: 1000
          max_per_ip: 20
```

| Field | Default | Description |
| --- | --- | --- |
| `max` | `0` | Maximum concurrent connections of the router, `0` is unlimited. |
| `max_per_ip` | `0` | Maximum concurrent connections of a client IP, `0` is unlimited. |

At least one of the fields is required. A connection is counted until the rest of the chain returns, so place `conn_limit` before the terminal middleware (`redirect`, `load_balancer`...).
