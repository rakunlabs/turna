# sni_allow_list

`sni_allow_list` passes TLS connections only for listed server names. It reads the SNI of the ClientHello without consuming it, so the TLS stream continues unchanged to `tls_terminate`, `redirect` or `load_balancer`.

```yaml
server:
  tcp:
    middlewares:
      allowed_hosts:
        sni_allow_list:
          sni:
            - api.example.com
            - "*.internal.example.com"
      backend:
        redirect:
          address: 10.0.0.1:443
    routers:
      tls:
        entrypoints: [tls]
        middlewares: [allowed_hosts, backend]
```

| Field | Default | Description |
| --- | --- | --- |
| `sni` | | Required. Allowed names. `*.example.com` matches any subdomain. |
| `allow_missing` | `false` | Pass TLS connections without SNI, e.g. clients connecting by IP. |
| `timeout` | `5s` | Time to read the ClientHello. |

Connections with other names, and non-TLS connections, are closed and logged at debug level.

To send names to different upstreams, use [`sni_router`](./sni_router).
