# log

`log` writes one log line when a connection closes, with the client and local address, duration and transferred bytes.

```yaml
server:
  tcp:
    middlewares:
      access:
        log:
          level: info
```

| Field | Default | Description |
| --- | --- | --- |
| `level` | `info` | Log level: `debug`, `info`, `warn` or `error`. Failed connections are logged at least at `warn`. |

Place it first in the router to include every connection. Bytes are counted at the position of the middleware, so before `tls_terminate` they include TLS overhead.
