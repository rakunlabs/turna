# IP Allow List

`ip_allow_list` drops UDP datagrams whose source address is not in the configured ranges. Use it before a terminal middleware such as `dns` or `redirect` to restrict which clients are served.

```yaml
server:
  udp:
    middlewares:
      local_only:
        ip_allow_list:
          source_range:
            - 127.0.0.1/32
            - 10.0.0.0/8
    routers:
      dns:
        entrypoints:
          - dns
        middlewares:
          - local_only
          - resolver
```

| Field | Description |
| --- | --- |
| `source_range` | List of allowed IPs or CIDR ranges. A datagram from any other source is dropped. |

When the source address is not allowed the middleware returns an error, which stops the chain and drops the datagram before any response is sent.
