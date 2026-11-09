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
            - "test:$2a$10$I/eN703FenkCIzqvSZ2UbeJFm.ddiTNIyXoO1ku2fUKkjSIddAKKi" # password: pass (bcrypt)
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

Generate a bcrypt (cost 10 or more) or argon2id hash and place it in `users`; plain text passwords are not accepted:

```sh
htpasswd -nbB -C 12 admin pass
```

Legacy APR1, md5crypt and `{SHA}` hashes need `allow_legacy_hashes: true`. See the [`basic_auth` reference](/reference/server/http/middlewares/basic_auth#hash-formats) for the full list.
