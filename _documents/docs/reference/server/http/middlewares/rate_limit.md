# rate_limit

`rate_limit` limits request throughput using `github.com/rakunlabs/ada/middleware/ratelimit`.

```yaml
server:
  http:
    middlewares:
      limit_by_ip:
        rate_limit:
          limit_type: ip
          requests: 100
          duration: 1m
```

| Field | Default | Description |
| --- | --- | --- |
| `limit_type` | `all` | `all`, `ip`, or `realip`. |
| `requests` | `100` | Number of requests allowed per duration. |
| `duration` | `1m` | Rate-limit window. |

`realip` keys on proxy-supplied headers (`True-Client-IP`, `X-Real-IP`, then the left-most `X-Forwarded-For`), falling back to the connection address. Make sure trusted proxy headers are correct before relying on it.
