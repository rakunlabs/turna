# SOCKS5 Proxy

This example exposes a SOCKS5 proxy on TCP port `1080` with username/password authentication and a hostname-to-IP override map for internal names.

```yaml
server:
  entrypoints:
    socks5:
      address: ":1080"
  tcp:
    middlewares:
      proxy:
        socks5:
          static_credentials:
            dev: dev-password
          ip_map:
            "*.internal.example.com": "10.0.10.1"
    routers:
      proxy:
        entrypoints:
          - socks5
        middlewares:
          - proxy
```

Test it with curl:

```sh
curl --socks5-hostname dev:dev-password@localhost:1080 http://app.internal.example.com/
```

`--socks5-hostname` sends the hostname to the proxy, so the `ip_map` glob rewrites `*.internal.example.com` to `10.0.10.1` on the proxy side.

For an open proxy on a trusted network, set `no_auth_authenticator: true` instead of `static_credentials`. Add the TCP [`ip_allow_list`](/reference/server/tcp/middlewares/ip_allow_list) middleware before `socks5` to restrict client IPs:

```yaml
tcp:
  middlewares:
    allow_lan:
      ip_allow_list:
        source_range:
          - 10.0.0.0/8
    proxy:
      socks5:
        no_auth_authenticator: true
  routers:
    proxy:
      entrypoints:
        - socks5
      middlewares:
        - allow_lan
        - proxy
```

See the [`socks5` reference](/reference/server/tcp/middlewares/socks5) for the DNS override field.
