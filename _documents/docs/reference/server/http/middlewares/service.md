# service

`service` is Turna's reverse proxy middleware. It can proxy to one or more upstream servers with round-robin balancing, or select upstreams by request path prefix.

```yaml
server:
  http:
    middlewares:
      backend:
        service:
          insecure_skip_verify: false
          pass_host_header: true
          loadbalancer:
            servers:
              - url: http://localhost:3000
              - url: http://localhost:3001
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `insecure_skip_verify` | `false` | Skip upstream TLS certificate verification. |
| `pass_host_header` | | When explicitly `false`, clear `r.Host` before proxying. |
| `loadbalancer.servers` | | Round-robin upstream list. |
| `prefixbalancer.prefixes` | | Path-prefix-specific upstream lists. |
| `prefixbalancer.default_servers` | | Default upstreams when no prefix matches. |

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
