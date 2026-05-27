# socks5

`socks5` exposes a SOCKS5 proxy on a TCP entrypoint.

```yaml
server:
  entrypoints:
    socks5:
      address: ":1080"
  tcp:
    middlewares:
      socks5:
        socks5:
          no_auth_authenticator: true
          static_credentials:
            admin: admin
          dns: ""
          ip_map:
            "*.internal.example.com": "10.0.10.1"
    routers:
      socks5:
        entrypoints:
          - socks5
        middlewares:
          - socks5
```

| Field | Description |
| --- | --- |
| `static_credentials` | Username/password map. |
| `no_auth_authenticator` | Allow unauthenticated SOCKS5 connections. |
| `dns` | Optional DNS server used for hostname resolution. |
| `ip_map` | Hostname glob to IP map. Matching uses doublestar glob patterns. |

Use a browser proxy plugin or a SOCKS-aware client to connect to the entrypoint.
