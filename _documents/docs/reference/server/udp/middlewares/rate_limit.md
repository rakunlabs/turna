# rate_limit

`rate_limit` limits datagrams per second of each client IP with a token bucket. Datagrams over the limit are dropped. Use it in front of `dns` to reduce amplification abuse.

```yaml
server:
  udp:
    middlewares:
      limit:
        rate_limit:
          rate: 50
          burst: 100
```

| Field | Default | Description |
| --- | --- | --- |
| `rate` | | Allowed datagrams per second. Required. |
| `burst` | `max(1, rate)` | Datagrams allowed at once. |
