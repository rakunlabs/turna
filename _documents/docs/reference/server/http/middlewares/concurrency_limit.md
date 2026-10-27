# concurrency_limit

`concurrency_limit` bounds the number of requests handled at the same time. Requests over the limit wait in a queue or get `503 Service Unavailable`.

Unlike [`rate_limit`](./rate_limit), which counts requests per time window, it protects slow endpoints and upstreams from too much parallel work.

```yaml
server:
  http:
    middlewares:
      export_limit:
        concurrency_limit:
          max: 4
          queue: 20
          queue_timeout: 30s
      tenant_limit:
        concurrency_limit:
          max: 10
          key:
            - header:X-Tenant-Id
```

| Field | Default | Description |
| --- | --- | --- |
| `max` | | Required. Requests handled at the same time per key. |
| `queue` | `0` | Requests per key waiting for a slot. `0` rejects requests over `max` right away. |
| `queue_timeout` | `10s` | Longest wait in the queue before the request is rejected. |
| `key` | | Parts to limit by, combined. Without it one limit applies to all requests. |
| `trusted_proxies` | | CIDRs whose forwarding headers are used for the `ip` key. |
| `status_code` | `503` | Status of rejected requests, e.g. `429`. |

Rejected requests get `Retry-After: 1`. Queued requests get a slot in arrival order. A client that disconnects while waiting leaves the queue.

## Keys

`key` accepts the same parts as `rate_limit`: `ip`, `realip`, `path`, `host`, `method`, `header:<name>`, `query:<name>` and `cookie:<name>`. A part missing in the request falls back to the client IP so leaving out a header does not escape the limit.

The limit is kept in memory per Turna instance and per named middleware.

Long-lived requests such as WebSockets and SSE hold a slot until they end. Leave them out of the route or give them their own limit.
