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

When `http.tls.store.default` is not configured, Turna generates a self-signed TLS 1.3 certificate for TLS routers.
