# maintenance

`maintenance` answers requests with a maintenance response, `503 Service Unavailable` by default, while it is enabled. Selected clients and paths still reach the application.

```yaml
server:
  http:
    middlewares:
      maintenance:
        maintenance:
          file: /var/run/turna/maintenance
          retry_after: 10m
          allow:
            - 10.0.0.0/8
          allow_paths:
            - /health
          bypass_header: X-Maintenance-Bypass
          bypass_value: change-me
          message: |
            <!doctype html>
            <title>Maintenance</title>
            <h1>We'll be back soon.</h1>
```

| Field | Default | Description |
| --- | --- | --- |
| `enabled` | `false` | Maintenance is on. |
| `file` | | Maintenance is on while this file exists. |
| `check_interval` | `1s` | How often `file` is checked. |
| `allow` | | IPs or CIDRs that still reach the application. |
| `trusted_proxies` | | CIDRs whose forwarding headers are used for `allow`. |
| `allow_paths` | | Path prefixes that still reach the application, e.g. health checks. |
| `bypass_header` | | Requests with this header set to `bypass_value` pass. |
| `bypass_value` | | Secret value of `bypass_header`. |
| `status_code` | `503` | Status of the response. |
| `retry_after` | | Sets `Retry-After`. |
| `message` | JSON message | Response body. |
| `content_type` | | Content type of `message`. Defaults to `text/html` when it starts with `<`, else `text/plain`. |

## Switching

- **Configuration:** set `enabled: true`. With a dynamic configuration source (Consul, a reloaded file) maintenance follows the source.
- **File:** set `file` and create or remove it, e.g. `touch /var/run/turna/maintenance` during a deploy. `enabled: true` keeps maintenance on regardless of the file.

Responses get `Cache-Control: no-store` so caches do not keep the maintenance page.
