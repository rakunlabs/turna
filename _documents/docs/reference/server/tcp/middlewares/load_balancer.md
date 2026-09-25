# load_balancer

`load_balancer` proxies each connection to one of several upstreams, with weighted round-robin, random or least-connections selection and active/passive health checks.

```yaml
server:
  tcp:
    middlewares:
      db:
        load_balancer:
          strategy: least_conn
          retry: 1
          dial_timeout: 3s
          servers:
            - address: 10.0.0.1:5432
              weight: 2
            - address: 10.0.0.2:5432
          health_check:
            interval: 10s
            timeout: 2s
            unhealthy_threshold: 2
          passive_health_check:
            max_fails: 3
            fail_timeout: 30s
```

| Field | Default | Description |
| --- | --- | --- |
| `servers[].address` | | Upstream `host:port`. |
| `servers[].weight` | `1` | Relative weight. |
| `strategy` | `round_robin` | `round_robin` (smooth weighted), `random` (weighted) or `least_conn` (open connections / weight). |
| `retry` | `0` | Dial other upstreams when dialing fails. |
| `dial_timeout` | `10s` | Dial timeout. |
| `disable_nagle` | `false` | Disable Nagle's algorithm. |
| `proxy_protocol` | `false` | Send a PROXY protocol header to the upstream. |
| `proxy_protocol_version` | `1` | `1` or `2`. |
| `health_check` | | Active check, see below. |
| `passive_health_check` | | Passive check, see below. |

## Health checks

The active check opens a TCP connection to every upstream:

| Field | Default | Description |
| --- | --- | --- |
| `interval` | `10s` | Time between checks. |
| `timeout` | `5s` | Dial timeout of the check. |
| `healthy_threshold` | `1` | Consecutive successes to mark healthy. |
| `unhealthy_threshold` | `1` | Consecutive failures to mark unhealthy. |

The passive check counts dial failures of real connections. After `max_fails` failures within `fail_timeout` the upstream is skipped for `fail_timeout`.

Unhealthy upstreams are skipped. When every upstream is unhealthy, turna still tries them (fail open).
