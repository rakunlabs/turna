# service

`service` is Turna's reverse proxy middleware. It can proxy to one or more upstream servers with weighted round-robin, random or least-connections balancing, health checks, session affinity and traffic mirroring, or select upstreams by request path prefix.

```yaml
server:
  http:
    middlewares:
      backend:
        service:
          insecure_skip_verify: false
          pass_host_header: true
          proxy: http://proxy.internal:3128
          loadbalancer:
            servers:
              - url: http://localhost:3000
              - url: http://localhost:3001
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `insecure_skip_verify` | `false` | Skip upstream TLS certificate verification. Also applies to active health checks and mirror requests. |
| `pass_host_header` | | When explicitly `false`, clear `r.Host` before proxying. |
| `proxy` | | Optional HTTP or HTTPS forward proxy URL used for upstream requests. Credentials may be included in the URL. |
| `loadbalancer.servers` | | Upstream list; each entry has `url` and optional `weight` (default `1`). |
| `loadbalancer.strategy` | `round_robin` | `round_robin` (smooth weighted), `random` (weighted) or `least_conn` (active requests / weight). |
| `loadbalancer.sticky.cookie` | | Session affinity cookie, see below. |
| `loadbalancer.health_check` | | Active health check, see below. |
| `loadbalancer.passive_health_check` | | Passive health check, see below. |
| `retry` | `0` | Retries on other upstreams when an upstream is unreachable. |
| `mirror` | | Shadow traffic, see below. |
| `prefixbalancer.prefixes` | | Path-prefix-specific upstream lists. |
| `prefixbalancer.default_servers` | | Default upstreams when no prefix matches. |

## WebSockets and streaming

`service` proxies WebSocket (`Connection: Upgrade`) and streaming responses (SSE) transparently. WebSocket upgrades are forwarded over the same transport as regular requests, so `wss://`/`https://` upstreams work, honoring `insecure_skip_verify`. Path rewrites and the `pass_host_header` setting apply to upgrades too.

WebSocket upgrades use HTTP/1.1 to the upstream; backends that only speak HTTP/2 cannot accept them.

## Prefix Balancer

```yaml
service:
  prefixbalancer:
    prefixes:
      - prefix: /api
        servers:
          - url: http://api:3000
      - prefix: /admin
        servers:
          - url: http://admin:3000
    default_servers:
      - url: http://web:3000
```

If the prefix balancer is configured, it is used instead of the plain `loadbalancer`.

The `loadbalancer` options (`strategy`, `sticky`, `health_check`, `passive_health_check`) also apply to every prefix group and the default servers. The same upstream URL used in several groups shares its health and connection state.

## Load Balancing

```yaml
service:
  retry: 1
  loadbalancer:
    strategy: least_conn
    servers:
      - url: http://app-1:3000
        weight: 3
      - url: http://app-2:3000
```

Unhealthy or ejected upstreams are skipped. When every upstream is unavailable, turna still tries them (fail open) instead of rejecting the request.

## Sticky Sessions

```yaml
loadbalancer:
  sticky:
    cookie:
      name: turna_lb
      max_age: 1h
      secure: true
```

| Field | Default | Description |
| --- | --- | --- |
| `name` | `turna_lb` | Cookie name. Prefix groups get a `_<index>` suffix. |
| `path` | `/` | Cookie path. |
| `domain` | | Cookie domain. |
| `max_age` | session | Cookie lifetime. |
| `secure` | `false` | `Secure` flag. |
| `http_only` | `true` | `HttpOnly` flag. |
| `same_site` | `lax` | `lax`, `strict` or `none`. |

The cookie holds a hash of the upstream URL. If that upstream is unhealthy, another one is picked and the cookie is updated.

## Active Health Check

```yaml
loadbalancer:
  health_check:
    path: /healthz
    interval: 10s
    timeout: 2s
    status: "200-299"
    unhealthy_threshold: 2
```

| Field | Default | Description |
| --- | --- | --- |
| `path` | `/` | Probe path, may include a query string. |
| `method` | `GET` | Probe method. |
| `host` | | Host header of the probe. |
| `headers` | | Extra probe headers. |
| `interval` | `10s` | Time between probes. |
| `timeout` | `5s` | Probe timeout. |
| `status` | `200-399` | Accepted status codes, e.g. `200,204,300-399`. |
| `healthy_threshold` | `1` | Consecutive successes to mark healthy. |
| `unhealthy_threshold` | `1` | Consecutive failures to mark unhealthy. |

## Passive Health Check

```yaml
loadbalancer:
  passive_health_check:
    max_fails: 3
    fail_timeout: 30s
    fail_statuses: [502, 503, 504]
```

An upstream is ejected for `fail_timeout` after `max_fails` failures within `fail_timeout`. Connection errors always count as failures; `fail_statuses` adds response codes.

## Mirror

Mirror sends a copy of requests to shadow upstreams. Mirror responses are discarded and never change the client response.

```yaml
service:
  loadbalancer:
    servers:
      - url: http://app-v1:3000
  mirror:
    servers:
      - url: http://app-v2:3000
        percent: 10
```

| Field | Default | Description |
| --- | --- | --- |
| `servers[].url` | | Shadow upstream. |
| `servers[].percent` | `100` | Percent of requests to mirror. |
| `max_body_size` | `1048576` | Requests with larger bodies are not mirrored. |
| `timeout` | `10s` | Mirror request timeout. |
| `max_in_flight` | `100` | Concurrent mirror requests; extra ones are dropped. |

WebSocket upgrades are not mirrored.
