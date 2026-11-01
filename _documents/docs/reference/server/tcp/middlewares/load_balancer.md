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
| `tls[].addresses` | | Upstream addresses connected over TLS. Other upstreams get the connection unchanged. |
| `tls[].tls` | | Client TLS of these upstreams, see below. |

With `tls`, the handshake with the upstream must finish within `dial_timeout`. A failed handshake closes the client connection and is not retried on another upstream.

| `tls[].tls` field | Description |
| --- | --- |
| `ca` | PEM bundle of trusted CA certificates; empty uses the system roots. |
| `server_name` | Sent as SNI and verified against the certificate unless `subject_alt_names` is set. |
| `subject_alt_names` | Names verified instead of `server_name`; DNS names may use a leading `*.`, values with a scheme are matched against URI SANs. |
| `client_cert`, `client_key` | PEM client certificate and key presented to the upstream. |

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
