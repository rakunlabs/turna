# session_limit

`session_limit` limits UDP sessions, in total and per client IP. A session is a client address (`ip:port`) that sent a datagram within `timeout`. Datagrams of new sessions over the limit are dropped; existing sessions keep working.

```yaml
server:
  udp:
    middlewares:
      sessions:
        session_limit:
          max: 10000
          max_per_ip: 50
          timeout: 1m
```

| Field | Default | Description |
| --- | --- | --- |
| `max` | `0` | Maximum sessions of the router, `0` is unlimited. |
| `max_per_ip` | `0` | Maximum sessions of a client IP, `0` is unlimited. |
| `timeout` | `60s` | A session without datagrams for this long ends. |

At least `max` or `max_per_ip` is required. Dropped datagrams are logged at debug level.

Use it before `load_balancer` so one client cannot fill its `max_sessions`. Set `timeout` near the `session_timeout` of the load balancer.
