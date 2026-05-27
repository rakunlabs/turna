# log

`log` writes a lightweight request log entry and continues the chain.

```yaml
server:
  http:
    middlewares:
      request_log:
        log:
          level: info
          message: incoming request
          headers: false
```

| Field | Default | Description |
| --- | --- | --- |
| `level` | `info` | One of `debug`, `info`, `warn`, or `error`. |
| `message` | | Log message. |
| `headers` | `false` | Include request headers in the log entry. |

For detailed request and response logging, use [`access_log`](./access_log).
