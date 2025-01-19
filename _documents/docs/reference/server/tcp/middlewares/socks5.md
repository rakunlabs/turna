# Socks5

Socks5 is a proxy protocol that allows a client to establish a connection to a server through a proxy server.

> Use `FoxyProxy` in `Firefox` to configure your browser to use a Socks5 proxy.

```yaml
server:
  tcp:
    middlewares:
      socks5:
        socks5:
          static_credentials:
            admin: admin # username: password list
          no_auth_authenticator: true
          dns: "" # DNS server to use for resolving hostnames, default is empty and will use the system DNS
          ip_map: # Useful for editing some hostnames to return different IP addresses without DNS resolution
            "*.kube.com": "10.0.10.1" # Map hostname to IP address
```

Example configuration:

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
          ip_map:
            "*.kube.com": "10.0.10.1"
    routers:
      socks5:
        entrypoints:
          - socks5
        middlewares:
          - socks5
```
