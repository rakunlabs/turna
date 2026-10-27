# bandwidth_limit

`bandwidth_limit` limits the bytes per second exchanged with each client IP. Datagrams over the limit are dropped.

```yaml
server:
  udp:
    middlewares:
      bandwidth:
        bandwidth_limit:
          upload: 1048576
          download: 4194304
```

| Field | Default | Description |
| --- | --- | --- |
| `upload` | `0` | Bytes per second received from a client IP, `0` is unlimited. |
| `download` | `0` | Bytes per second of replies to a client IP, `0` is unlimited. |
| `burst` | rate, at least `65535` | Bytes that can be sent at once. |

At least `upload` or `download` is required. A datagram larger than `burst` is always dropped, so keep it at least as large as the biggest datagram.

The download limit applies to replies sent by the middlewares after it, such as `redirect`, `load_balancer` and `dns`. Place `bandwidth_limit` before them.

For TCP see [`bandwidth_limit`](../../tcp/middlewares/bandwidth_limit), which slows connections down instead of dropping data.
