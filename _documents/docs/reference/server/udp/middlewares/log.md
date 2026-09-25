# log

`log` writes one log line for every datagram with the client address and size, then continues the chain.

```yaml
server:
  udp:
    middlewares:
      access:
        log:
          level: info
```

| Field | Default | Description |
| --- | --- | --- |
| `level` | `debug` | Log level: `debug`, `info`, `warn` or `error`. |

UDP traffic can be high; the default `debug` level keeps it out of normal logs.
