# cache

`cache` stores upstream responses and serves them again without calling the upstream. It follows the `Cache-Control` headers of the upstream and keeps entries in memory or in Redis.

```yaml
server:
  http:
    middlewares:
      api_cache:
        cache:
          ttl: 30s
          max_ttl: 10m
          stale_while_revalidate: 1m
          stale_if_error: 1h
          max_size: 128MB
      shared_cache:
        cache:
          redis:
            address:
              - localhost:6379
    routers:
      api:
        path: /api/*
        middlewares:
          - api_cache
          - backend
```

| Field | Default | Description |
| --- | --- | --- |
| `ttl` | `0` | Lifetime of responses without `max-age`, `s-maxage` or `Expires`. `0` does not store them. |
| `max_ttl` | | Caps the lifetime set by the upstream. |
| `stale_while_revalidate` | `0` | Serve an expired response while it is refreshed in the background. |
| `stale_if_error` | `0` | Serve an expired response when the upstream fails or answers `5xx`. |
| `methods` | `GET, HEAD` | Cached methods. `HEAD` is answered from stored `GET` responses. |
| `statuses` | `200, 203, 204, 300, 301, 308, 404, 405, 410, 414, 501` | Cached statuses. |
| `max_body_size` | `1MB` | Largest stored response body. |
| `key_headers` | | Request headers added to the cache key, e.g. `X-Tenant-Id`. |
| `cache_authorized` | `false` | Store responses of requests with `Authorization` even without `public`. |
| `ignore_request_cache_control` | `false` | Ignore `Cache-Control: no-cache` sent by clients. |
| `status_header` | `X-Cache` | Response header with `HIT`, `MISS`, `STALE` or `BYPASS`. `-` disables it. |
| `max_size` | `64MB` | Size of the memory cache. The least recently used entries are removed first. |
| `redis` | | Share the cache between instances, see below. |

## What is stored

The cache key is the scheme, host, path and query plus `key_headers`. Responses with `Vary` are stored per value of the listed request headers.

A response is not stored when:

- `Cache-Control` has `no-store`, `no-cache` or `private`,
- it sets a cookie or has `Vary: *`,
- the request has `Authorization`, unless the response is `public` or has `s-maxage`, or `cache_authorized` is set,
- its status is not in `statuses` or the body is larger than `max_body_size`.

`s-maxage` wins over `max-age`, which wins over `Expires`. The upstream's `stale-while-revalidate` and `stale-if-error` directives override the configured values, and `must-revalidate` disables both.

## Requests

- Clients sending `Cache-Control: no-cache` or `max-age=0` skip the stored response and the new response is stored.
- `If-None-Match` and `If-Modified-Since` get `304 Not Modified` from a stored response.
- `Range` requests and WebSocket upgrades pass through.
- A successful `POST`, `PUT`, `PATCH` or `DELETE` removes the stored responses of the same URL.

Without `stale_while_revalidate`, concurrent misses of the same URL all reach the upstream.

## Redis

| Field | Default | Description |
| --- | --- | --- |
| `redis.address` | | Redis addresses. More than one selects Redis Cluster. |
| `redis.username` | | Username. |
| `redis.password` | | Password. |
| `redis.cluster` | `false` | Force Redis Cluster mode. |
| `redis.tls` | | `enabled`, `cert_file`, `key_file`, `ca_file`. |
| `redis.key_prefix` | `turna_cache_` | Prefix of the cache keys. |

Entries expire with Redis TTLs, so `max_size` does not apply; set a `maxmemory` policy on Redis. When Redis is unavailable requests go to the upstream.

Put `cache` after authentication middlewares and `compress`, and before `service`.
