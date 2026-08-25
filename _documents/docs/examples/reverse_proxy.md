# Reverse Proxy

This example is a small API gateway: `/api/*` is proxied to two backends with round-robin balancing, everything else serves a SPA build. The API route gets CORS, gzip, an IP rate limit, and an access log.

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      access:
        access_log: {}

      api_cors:
        cors:
          allow_origins:
            - http://localhost:3000
          allow_headers:
            - Authorization
            - Content-Type

      api_limit:
        rate_limit:
          limit_type: ip
          requests: 100
          duration: 1m

      compress:
        gzip: {}

      api_strip:
        strip_prefix:
          prefix: /api

      backend:
        service:
          loadbalancer:
            servers:
              - url: http://localhost:9090
              - url: http://localhost:9091

      frontend:
        folder:
          path: ./dist
          index: true
          spa: true

    routers:
      api:
        path: /api/*
        middlewares:
          - access
          - api_cors
          - api_limit
          - compress
          - api_strip
          - backend
      app:
        path: /*
        middlewares:
          - compress
          - frontend
```

A request to `http://localhost:8080/api/users` is forwarded as `http://localhost:9090/users` (then `:9091`, alternating). WebSocket and SSE upstreams work transparently through `service`.

## Routing by prefix instead of routers

When several backends live behind one route, `prefixbalancer` selects the upstream by path prefix inside a single `service` middleware:

```yaml
backend:
  service:
    prefixbalancer:
      prefixes:
        - prefix: /api
          servers:
            - url: http://api:3000
        - prefix: /admin
          servers:
            - url: http://admin:3000
      default_servers:
        - url: http://web:3000
```

See the [`service` reference](/reference/server/http/middlewares/service) for `pass_host_header`, TLS verification, and streaming details.
