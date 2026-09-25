# load_balancer

`load_balancer` forwards datagrams to one of several UDP upstreams. Each client address gets a session bound to one upstream, so all datagrams of a client reach the same upstream and every reply of the upstream is sent back. Unlike [`redirect`](./redirect), protocols with several replies per request (game servers, QUIC, syslog, WireGuard...) work.

```yaml
server:
  udp:
    middlewares:
      dns_backends:
        load_balancer:
          strategy: round_robin
          session_timeout: 30s
          servers:
            - address: 10.0.0.1:53
              weight: 2
            - address: 10.0.0.2:53
          health_check:
            interval: 10s
            timeout: 2s
            payload: "ping"
            expect: "pong"
          passive_health_check:
            max_fails: 3
            fail_timeout: 30s
```

| Field | Default | Description |
| --- | --- | --- |
| `servers[].address` | | Upstream `host:port`. |
| `servers[].weight` | `1` | Relative weight. |
| `strategy` | `round_robin` | `round_robin`, `random` or `least_conn` (open sessions / weight). |
| `network` | `udp` | `udp`, `udp4` or `udp6`. |
| `session_timeout` | `30s` | Sessions without traffic are closed. |
| `max_sessions` | `10000` | Maximum open sessions; new clients are dropped above it. |
| `health_check.payload` | | Datagram sent to every upstream. Required for the active check. |
| `health_check.expect` | | Substring the reply must contain; empty accepts any reply. |
| `health_check.interval` | `10s` | Time between checks. |
| `health_check.timeout` | `5s` | Time to wait for the reply. |
| `health_check.healthy_threshold` | `1` | Consecutive successes to mark healthy. |
| `health_check.unhealthy_threshold` | `1` | Consecutive failures to mark unhealthy. |
| `passive_health_check.max_fails` | | Send/receive errors (e.g. ICMP port unreachable) within `fail_timeout` that eject the upstream. |
| `passive_health_check.fail_timeout` | `10s` | Counting window and ejection time. |

Unhealthy upstreams are skipped. When every upstream is unhealthy, turna still tries them (fail open). `load_balancer` is terminal; place filters such as `rate_limit` before it.
