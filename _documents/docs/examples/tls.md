# TLS

This example serves the same `hello` response on one HTTP entrypoint and one HTTPS entrypoint.

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
    websecure:
      address: ":8443"
  http:
    middlewares:
      hello:
        hello:
          message: hello from turna
    routers:
      https:
        entrypoints:
          - websecure
        path: /
        tls: {}
        middlewares:
          - hello
      http:
        entrypoints:
          - web
        path: /
        middlewares:
          - hello
```

When `http.tls.store.default` is not configured, Turna generates a self-signed TLS 1.3 certificate (valid for `localhost`, `127.0.0.1`, and `::1`) for TLS routers.

To serve several host names from one entrypoint, add more `store` keys; the certificate is chosen per request by SNI, with `default` as the fallback. The minimum TLS version is configurable via `http.tls.min_version` (`1.2` or `1.3`, default `1.3`). See the [server reference](/reference/server/server#tls).

## ACME (Let's Encrypt)

Instead of supplying certificate files, Turna can obtain and renew certificates automatically from an ACME CA such as Let's Encrypt, using the TLS-ALPN-01 challenge over the existing TLS entrypoint (the entrypoint must be publicly reachable, usually `:443`).

```yaml
server:
  entrypoints:
    websecure:
      address: ":443"
  http:
    tls:
      acme:
        enabled: true
        email: admin@example.com
        domains:
          - app.example.com
        cache_dir: ./acme-cache
        # Use the staging CA while testing to avoid rate limits.
        directory_url: "https://acme-staging-v02.api.letsencrypt.org/directory"
    routers:
      secure:
        entrypoints:
          - websecure
        path: /*
        tls: {}
        middlewares:
          - hello
```

See the [server reference](/reference/server/server#acme-lets-encrypt) for all ACME fields.
