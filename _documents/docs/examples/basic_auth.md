# Basic Auth

This example protects `/private/*` with `basic_auth` and serves the same folder publicly for every other path.

```yaml
server:
  entrypoints:
    web:
      address: ":8000"
  http:
    middlewares:
      private_auth:
        basic_auth:
          users:
            - "test:$apr1$JMWtQHoL$g/5ey5x7psJM7htuB6OEy0" # password: pass
          remove_header: true
      files:
        folder:
          path: ./
          browse: true
          utc: true
    routers:
      private:
        path: /private/*
        middlewares:
          - private_auth
          - files
      public:
        path: /*
        middlewares:
          - files
```

Generate htpasswd-compatible hashes with any APR1/htpasswd tool and place them in `users`.
