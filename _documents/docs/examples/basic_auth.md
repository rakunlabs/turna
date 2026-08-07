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
            - "test:$apr1$JMWtQHoL$g/5ey5x7psJM7htuB6OEy0" # password: pass (APR1)
            - "admin:$2y$10$kcIcZZWz2YciwMVS9UYJaurYifLlMfzZ6WHL/SoDtTvENTQMi8ii2" # password: pass (bcrypt)
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

Generate htpasswd-compatible hashes and place them in `users`. bcrypt (`$2y$`/`$2b$`/`$2a$`/`$2x$`), Apache MD5 (`$apr1$`), md5crypt (`$1$`) and SHA-1 (`{SHA}`) are supported; plain text passwords are not. bcrypt is recommended:

```sh
htpasswd -nbB admin pass
```

See the [`basic_auth` reference](/reference/server/http/middlewares/basic_auth#hash-formats) for the full list.
