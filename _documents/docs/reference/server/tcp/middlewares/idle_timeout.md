# idle_timeout

`idle_timeout` closes connections without traffic and can limit the total connection lifetime.

```yaml
server:
  tcp:
    middlewares:
      timeouts:
        idle_timeout:
          timeout: 5m
          max_duration: 12h
```

| Field | Default | Description |
| --- | --- | --- |
| `timeout` | | Close the connection when no data is read from or written to the client for this duration. |
| `max_duration` | | Close the connection after this duration regardless of traffic. |

At least one of the fields is required. Traffic in either direction keeps the connection alive.
