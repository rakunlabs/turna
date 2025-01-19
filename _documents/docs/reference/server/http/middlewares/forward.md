# Forward

Forward HTTP proxy middleware is used to forward the request in the server.

> Use this like `http_proxy` variable in the environment.

```yaml
server:
  http:
    middlewares:
      forward:
        forward:
          insecure_skip_verify: false # default is false, bool, skip verify the certificate
```

Example configuration:

```yaml
server:
  entrypoints:
    web:
      address: ":9292"
  http:
    middlewares:
      forward:
        forward: {}
    routers:
      project:
        path: /*
        # tls: {}
        middlewares:
          - forward
```
