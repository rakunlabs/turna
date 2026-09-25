# rate_limit

`rate_limit` limits new connections per second of each client IP with a token bucket. Connections over the limit are closed immediately.

```yaml
server:
  tcp:
    middlewares:
      slow_down:
        rate_limit:
          rate: 5
          burst: 20
```

| Field | Default | Description |
| --- | --- | --- |
| `rate` | | Allowed new connections per second. Required, may be fractional (`0.5` is one every two seconds). |
| `burst` | `max(1, rate)` | Connections allowed at once. |

Idle client entries are removed automatically.
