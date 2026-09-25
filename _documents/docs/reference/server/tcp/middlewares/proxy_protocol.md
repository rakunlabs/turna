# proxy_protocol

`proxy_protocol` reads an incoming [PROXY protocol](https://www.haproxy.org/download/2.9/doc/proxy-protocol.txt) v1 or v2 header, for example from HAProxy, AWS NLB or another turna, and replaces the client address of the connection. The next middlewares (`ip_allow_list`, `rate_limit`, `log`, `redirect` with `proxy_protocol`...) see the original client.

```yaml
server:
  tcp:
    middlewares:
      from_lb:
        proxy_protocol:
          trusted_ips:
            - 10.0.0.0/8
          optional: false
          timeout: 5s
    routers:
      app:
        entrypoints: [app]
        middlewares: [from_lb, allow_office, backend]
```

| Field | Default | Description |
| --- | --- | --- |
| `trusted_ips` | | IPs or CIDRs allowed to send a header. Required. Headers are never read from other peers; their connections continue with the peer address. |
| `optional` | `false` | Accept connections without a header from trusted peers. When `false`, such connections are closed. |
| `timeout` | `5s` | Timeout to read the header. |

Both versions are detected automatically. v2 `LOCAL` and v1 `UNKNOWN` headers (health checks) keep the peer address.

To send a header to an upstream, use `proxy_protocol` on [`redirect`](./redirect) or [`load_balancer`](./load_balancer).
