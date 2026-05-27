# cors

`cors` adds CORS response headers and handles preflight requests.

```yaml
server:
  http:
    middlewares:
      cors_api:
        cors:
          allow_origins:
            - https://app.example.com
          allow_methods:
            - GET
            - POST
          allow_headers:
            - Authorization
            - Content-Type
          expose_headers:
            - X-Request-Id
          max_age: 600
          allow_credentials: true
```

| Field | Default | Description |
| --- | --- | --- |
| `allow_origins` | `['*']` | Allowed origins. `*` and `?` wildcards are supported. |
| `allow_methods` | `GET,HEAD,PUT,PATCH,POST,DELETE` | Methods sent in preflight responses. |
| `allow_headers` | | Headers allowed in preflight responses. When empty, requested headers are echoed. |
| `expose_headers` | | Headers exposed to browser JavaScript. |
| `max_age` | `0` | Preflight cache duration in seconds. |
| `allow_credentials` | `false` | Set `Access-Control-Allow-Credentials: true`. |
| `unsafe_wildcard_origin_with_allow_credentials` | `false` | Reflect any origin when credentials and `*` are both used. This is unsafe. |
