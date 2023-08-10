# Server

Turna can has server option, this option can be used for reverse proxy or load balancer with various middlewares.

## Configuration

Highly inspired from [traefik](https://doc.traefik.io/traefik/) with small differences.

Turna has `entrypoints`, `http` section. Under _http_ section, there are `middlewares`, `routers` and `tls`.

```yaml
server:
  # load_value: "x-server"
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares: {}
    routers:
      test:
        # entrypoints:
        #   - web
        # tls: {}
        path: /test
        middlewares:
          - test
          - service
```

### EntryPoints

entrypoints are the connection point to the server. It can be usable with routers.  
It is a map of entrypoint name and entrypoint configuration.

```yaml
entrypoints:
  web:
    address: ":8080"
```

### Routers

Routers combine entrypoints and middlewares with a path.

```yaml
routers:
  test:
    entrypoints:
      - web
    host: "example.com" # optional to use with host rule
    path: /*
    middlewares:
      - test
      - service
```

### Middlewares

Middlewares are the main part of the server. It can be used for authentication, authorization, rate limiting, etc.

Declare middlewares under `http.middlewares` section and use them in the routers with order.

### TLS

TLS is the configuration for the TLS connection. It can be used with routers.

Don't mix entypoint for TLS and non-TLS routers. Serve will fail.

```yaml
http:
  middlewares: {}
  routers: {}
  tls:
    default: # default tls certificate
      - cert_file: ""
        key_file: ""
```

To enable tls, add `tls` section to the router.

```yaml
routers:
  test:
    entrypoints:
      - web
    host: "example.com" # optional to use with host rule
    path: /*
    middlewares:
      - test
      - service
    tls: {}
```
