# access_log

`access_log` writes structured request and response logs. It can include headers, bodies, duration, response size, and per-path logging rules.

```yaml
server:
  http:
    middlewares:
      access:
        access_log:
          level: info
          message: access log
          skip_sse: true
          path:
            enabled:
              - url: /**
                methods: ["*"]
            disabled:
              - url: /health
          log_details:
            request_body: true
            request_body_size: 512
            response_body: true
            response_body_size: 512
            headers: true
            sanitize_headers:
              - Authorization
              - Cookie
              - Set-Cookie
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `level` | `info` | One of `debug`, `info`, `warn`, or `error`. |
| `message` | `access log` | Log message. |
| `skip_sse` | `true` | Skip buffering and logging for `Accept: text/event-stream`. |
| `path.enabled` | | Paths to log. If no enabled rule matches, the request is not logged. |
| `path.disabled` | | Paths to skip before checking enabled rules. |
| `log_details` | | Global detail settings used by enabled rules unless overridden. |

Path checks use doublestar patterns. Method rules accept `*`, explicit methods, `+METHOD` to allow, and `-METHOD` to block.

`request_body_size` and `response_body_size` are byte limits. A value of `0` means no limit. Default sensitive headers are `Authorization`, `Cookie`, `Set-Cookie`, and `X-Forwarded-For`.
