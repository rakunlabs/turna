# cors

Use echo's CORS middleware.

```yaml
middlewares:
  test:
    cors:
      allow_origins:
        - http://foo.com
        - http://bar.com
      allow_methods:
        - GET
        - POST
      allow_headers:
        - X-Header
      expose_headers:
        - X-Header
      max_age: 10
      allow_credentials: true
      unsafe_wildcard_origin_with_allow_credentials: true
```
