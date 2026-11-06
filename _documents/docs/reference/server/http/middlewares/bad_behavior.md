# bad_behavior

`bad_behavior` bans clients that get many error responses in a short time, such as scanners probing for paths.

```yaml
server:
  http:
    middlewares:
      ban_scanners:
        bad_behavior:
          threshold: 30
          window: 1m
          ban_time: 24h
          allow:
            - 10.0.0.0/8
          trusted_proxies:
            - 172.16.0.0/12
```

| Field | Default | Description |
| --- | --- | --- |
| `status_codes` | `400,401,403,404,405,429,444` | Response statuses counted as bad. Codes or ranges like `500-599`. |
| `threshold` | `30` | Bad responses within `window` that ban the client. |
| `window` | `1m` | Counting window, starting at the first bad response of a client. |
| `ban_time` | `24h` | How long a client stays banned. Must be greater than 0. |
| `status_code` | `403` | Status of requests from banned clients. |
| `allow` | | IPs or CIDRs that are never counted or banned. |
| `trusted_proxies` | | CIDRs of proxies in front of Turna, see [ip_filter](./ip_filter#client-ip). |
| `redis` | | Share counters and bans between Turna instances, see below. Default is in memory. |
| `fail_closed` | `false` | Reject with `503` when Redis is unavailable instead of letting requests through. |

Requests of a banned client are rejected before they reach the next handler and get a `Retry-After` header with the remaining ban time. A ban is logged as a warning with the client IP.

Place `bad_behavior` before the middlewares and services whose responses should be counted. Responses written by middlewares placed before it are not seen.

The in-memory counters and bans are dropped when the configuration is reloaded.

## Redis

```yaml
bad_behavior:
  redis:
    address:
      - redis:6379
    password: secret
    key_prefix: turna_badbehavior_
```

| Field | Default | Description |
| --- | --- | --- |
| `redis.address` | | Server addresses. More than one selects Redis Cluster. |
| `redis.username` / `redis.password` | | Credentials. |
| `redis.cluster` | `false` | Force cluster mode for a single address; otherwise it is detected. |
| `redis.gcp_iam` | `false` | Authenticate with a Google Cloud access token (Memorystore IAM), see [session](./session). Leave username and password empty. |
| `redis.tls` | | `enabled`, `cert_file`, `key_file`, `ca_file`. |
| `redis.key_prefix` | `turna_badbehavior_` | Prefix of the counter and ban keys. |

Redis must be reachable at startup. If it fails later, requests pass unless `fail_closed` is set.

`bad_behavior.redis` cannot be set from Docker labels.
