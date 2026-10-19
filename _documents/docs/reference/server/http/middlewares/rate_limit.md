# rate_limit

`rate_limit` limits request throughput with a sliding window per key.

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
| `limit_type` | `all` | `all`, `ip`, or `realip`. Ignored when `key` is set. |
| `requests` | `100` | Number of requests allowed per duration. |
| `duration` | `1m` | Rate-limit window. |
| `key` | | What requests are counted by, see below. Several parts are combined. |
| `trusted_proxies` | | CIDRs whose `X-Forwarded-For` / `X-Real-IP` headers are used for the `ip` key. |
| `headers` | `false` | Add `X-RateLimit-Limit`, `X-RateLimit-Remaining` and `X-RateLimit-Reset` to responses. |
| `redis` | | Share counters between Turna instances, see below. Default is in memory. |
| `fail_closed` | `false` | Reject with `503` when Redis is unavailable instead of letting requests through. |

Rejected requests get `429 Too Many Requests` with a `Retry-After` header.

`ip` uses the connection address, or the client behind one of `trusted_proxies`. `realip` keys on proxy-supplied headers (`True-Client-IP`, `X-Real-IP`, then the left-most `X-Forwarded-For`) from every peer, falling back to the connection address; clients can forge it unless a proxy in front always overwrites these headers. Prefer `ip` with `trusted_proxies`.

## Keys

| Key | Counted by |
| --- | --- |
| `ip` | Client IP, see `trusted_proxies`. |
| `realip` | Client IP from forwarding headers of any peer. |
| `path` | Request path. |
| `host` | Request host. |
| `method` | Request method. |
| `header:<name>` | Header value, e.g. `header:X-API-Key`. |
| `query:<name>` | Query parameter value. |
| `cookie:<name>` | Cookie value. |

A part that is missing in the request falls back to the client IP, so leaving out a header does not escape the limit.

```yaml
rate_limit:
  key:
    - header:X-API-Key
    - path
  requests: 1000
  duration: 1h
  headers: true
```

## Redis

With several Turna instances behind a load balancer, each in-memory limiter counts on its own. Redis shares the counters:

```yaml
rate_limit:
  limit_type: ip
  requests: 100
  duration: 1m
  redis:
    address:
      - redis:6379
    password: secret
    key_prefix: turna_ratelimit_
```

| Field | Default | Description |
| --- | --- | --- |
| `redis.address` | | Server addresses. More than one selects Redis Cluster. |
| `redis.username` / `redis.password` | | Credentials. |
| `redis.cluster` | `false` | Force cluster mode for a single address; otherwise it is detected. |
| `redis.tls` | | `enabled`, `cert_file`, `key_file`, `ca_file`. |
| `redis.key_prefix` | `turna_ratelimit_` | Prefix of the counter keys. |

Redis must be reachable at startup. If it fails later, requests pass unless `fail_closed` is set.

`rate_limit.redis` cannot be set from Docker labels.
