# Server

The `server` section defines network listeners, HTTP routers, TCP routers, middleware, and TLS certificates.

```yaml
server:
  load_value: ""
  entrypoints:
    web:
      address: ":8080"
      network: tcp
  http:
    tls:
      store: {}
    middlewares: {}
    routers: {}
  tcp:
    middlewares: {}
    routers: {}
```

## Entrypoints

Entrypoints are named listeners. Routers attach to one or more entrypoints.

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
      network: tcp
    docker:
      address: ":2375"
      network: tcp
```

| Field | Default | Description |
| --- | --- | --- |
| `address` | | Address passed to `net.Listen`, such as `:8080` or `/var/run/app.sock`. |
| `network` | `tcp` | Network passed to `net.Listen`. |

## HTTP Routers

HTTP routers match a host, path, and entrypoint, then run an ordered middleware chain.

```yaml
server:
  http:
    routers:
      app:
        host: example.com
        path:
          - /api/*
        entrypoints:
          - web
        middlewares:
          - strip_api
          - app_service
        tls: {}
        pre_middlewares:
          request_id: true
          server_info: true
```

| Field | Description |
| --- | --- |
| `host` | Optional host rule. The port is stripped before matching. Empty host acts as a fallback for the entrypoint. |
| `path` | One or more chi route patterns. |
| `entrypoints` | Listener names. Defaults to all listeners. |
| `middlewares` | Ordered middleware names from `server.http.middlewares`. |
| `tls` | Enable TLS on this router when present. |
| `pre_middlewares.request_id` | Enable built-in request ID middleware. Default is true. |
| `pre_middlewares.server_info` | Enable built-in `Server` response header. Default is true. |

Each router always includes panic recovery, Turna request context setup, optional pre-middlewares, configured middlewares, and a final `204 No Content` fallback.

## HTTP Middlewares

Declare middleware instances under `server.http.middlewares`, then reference their names from routers.

```yaml
server:
  http:
    middlewares:
      strip_api:
        strip_prefix:
          prefix: /api
      app_service:
        service:
          loadbalancer:
            servers:
              - url: http://localhost:3000
    routers:
      app:
        path: /api/*
        middlewares:
          - strip_api
          - app_service
```

A single named middleware object uses the first configured middleware type in the registry. Do not put multiple middleware types under one middleware name; create separate named middleware entries and chain them in the router.

See the [HTTP middleware index](./http/middlewares/) for all supported keys.

## TLS

Add `tls: {}` to a router to serve that router over TLS. Do not mix TLS and non-TLS routers on the same entrypoint.

```yaml
server:
  entrypoints:
    websecure:
      address: ":8443"
  http:
    tls:
      min_version: "1.3"
      store:
        default:
          - cert_file: ./cert.pem
            key_file: ./key.pem
        app.example.com:
          - cert_file: ./app.pem
            key_file: ./app-key.pem
        "*.internal.example.com":
          - cert_file: ./internal.pem
            key_file: ./internal-key.pem
    routers:
      secure:
        entrypoints:
          - websecure
        path: /*
        tls: {}
        middlewares:
          - app
```

| Field | Default | Description |
| --- | --- | --- |
| `min_version` | `1.3` | Minimum accepted TLS version: `1.2` or `1.3`. |
| `store.<host>` | | Certificate(s) served for the SNI host name `<host>`. |
| `store.default` | | Fallback certificate used when no SNI host matches or the client sends no SNI. |
| `self_signed` | | Customizes the generated certificate (see below). |

### SNI (multiple certificates)

`store` keys are SNI host names. On each TLS handshake the certificate is selected by the client's SNI server name:

1. exact host match (e.g. `app.example.com`),
2. wildcard match by replacing the first label (e.g. `api.internal.example.com` matches `*.internal.example.com`),
3. the `default` host key,
4. a generated self-signed certificate when nothing else matches.

### Self-signed certificate

If no certificate is configured for a matched host, Turna generates a self-signed certificate. By default it is valid for `localhost`, `127.0.0.1`, and `::1`. Extend it with `self_signed`:

```yaml
server:
  http:
    tls:
      self_signed:
        organization:
          - turna
        dns_names:
          - dev.local
        ips:
          - 192.168.1.10
```

## TCP Routers

TCP routers attach TCP middleware chains to TCP entrypoints.

```yaml
server:
  entrypoints:
    docker:
      address: ":2375"
  tcp:
    middlewares:
      local_only:
        ip_allow_list:
          source_range:
            - 127.0.0.1/32
      docker_socket:
        redirect:
          address: /var/run/docker.sock
          network: unix
    routers:
      docker:
        entrypoints:
          - docker
        middlewares:
          - local_only
          - docker_socket
```

TCP middleware runs sequentially for each accepted connection. If a middleware returns an error, the chain stops and the connection is closed.
