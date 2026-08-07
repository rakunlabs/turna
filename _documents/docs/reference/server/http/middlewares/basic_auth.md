# basic_auth

`basic_auth` protects a route with HTTP Basic authentication. Users are configured as `username:htpasswd_hash` entries and are checked with `github.com/abbot/go-http-auth`.

```yaml
server:
  http:
    middlewares:
      private_auth:
        basic_auth:
          realm: Restricted
          users:
            - "test:$2y$10$kcIcZZWz2YciwMVS9UYJaurYifLlMfzZ6WHL/SoDtTvENTQMi8ii2"
          header_field: X-User
          remove_header: true
```

| Field | Default | Description |
| --- | --- | --- |
| `users` | | List of `username:hash` credentials. See [Hash formats](#hash-formats). |
| `realm` | `Restricted` | Realm sent in `WWW-Authenticate`. |
| `header_field` | `X-User` | Request header set to the authenticated username. Set an empty value to disable. |
| `remove_header` | `false` | Remove the original `Authorization` header after successful authentication. |

## Hash formats

The hash algorithm is selected from the prefix of the stored value.

| Prefix | Algorithm | Generate with |
| --- | --- | --- |
| `$2y$`, `$2b$`, `$2a$`, `$2x$` | bcrypt | `htpasswd -nbB user pass` |
| `$apr1$` | Apache MD5 (APR1) | `htpasswd -nbm user pass` |
| `$1$` | md5crypt | `openssl passwd -1 pass` |
| `{SHA}` | SHA-1 + base64 | `htpasswd -nbs user pass` |

Notes:

- Plain text passwords are **not** supported. A value without a known prefix is treated as md5crypt and always fails, which surfaces as a silent `401`.
- Classic crypt(3) DES hashes (`htpasswd -d`) are **not** supported.
- Each entry is split on `:` and must yield exactly two parts, otherwise the middleware fails to start with `invalid user`. None of the formats above contain `:`, so this only matters for hand-written values.
- bcrypt is the recommended choice; APR1, md5crypt and SHA-1 exist for htpasswd compatibility only.
