# DNS Server

This example answers DNS on UDP port `5353` for the `example.com` zone from static records and forwards everything else to public resolvers.

```yaml
server:
  entrypoints:
    dns:
      address: ":5353"
  udp:
    middlewares:
      resolver:
        dns:
          origin: example.com
          ttl: 300
          records:
            - "@ IN A 10.0.0.1"
            - "www IN A 10.0.0.2"
            - "alias IN CNAME www"
            - "*.dev IN A 10.0.0.9"
          upstream:
            - 1.1.1.1:53
            - 8.8.8.8:53
    routers:
      resolver:
        entrypoints:
          - dns
        middlewares:
          - resolver
```

Test it with `dig`:

```sh
dig @127.0.0.1 -p 5353 www.example.com        # 10.0.0.2
dig @127.0.0.1 -p 5353 anything.dev.example.com # 10.0.0.9 (wildcard)
dig @127.0.0.1 -p 5353 google.com             # forwarded upstream
```

Names inside the zone that do not match return authoritative `NXDOMAIN`; names outside the zone are forwarded to `upstream`.

To restrict which clients may query, add the UDP [`ip_allow_list`](/reference/server/udp/middlewares/ip_allow_list) middleware before `dns` in the router chain:

```yaml
udp:
  middlewares:
    allow_lan:
      ip_allow_list:
        source_range:
          - 10.0.0.0/8
          - 127.0.0.1/32
    resolver:
      dns:
        # ...
  routers:
    resolver:
      entrypoints:
        - dns
      middlewares:
        - allow_lan
        - resolver
```

See the [`dns` reference](/reference/server/udp/middlewares/dns) for record syntax, wildcards, and resolution order.
