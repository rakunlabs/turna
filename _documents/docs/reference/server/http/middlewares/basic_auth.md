# basic_auth

`basic_auth` protects a route with HTTP Basic authentication. Users are configured as `username:hash` entries; only slow, salted hashes (bcrypt, argon2id) are accepted by default.

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
| `users` | | List of `username:hash` credentials. The entry is split on the first `:`. See [Hash formats](#hash-formats). |
| `realm` | `Restricted` | Realm sent in `WWW-Authenticate`. |
| `header_field` | `X-User` | Request header set to the authenticated username. A client-sent value is always overwritten. |
| `remove_header` | `false` | Remove the original `Authorization` header after successful authentication. |
| `allow_legacy_hashes` | `false` | Also accept APR1, md5crypt and `{SHA}` hashes. Each one logs a warning at startup. Use it only while migrating. |

## Hash formats

Hashes are parsed at startup; an unsupported or weak hash stops the middleware with an error naming the user.

| Prefix | Algorithm | Requirement | Generate with |
| --- | --- | --- | --- |
| `$2y$`, `$2b$`, `$2a$`, `$2x$` | bcrypt | cost ≥ 10 | `htpasswd -nbB -C 12 user pass` |
| `$argon2id$v=19$` | Argon2id (PHC string) | m ≥ 19456, t ≥ 1, p ≥ 1 | `echo -n pass \| argon2 "$(openssl rand -base64 16)" -id -t 3 -m 16 -p 4 -e` |
| `$apr1$` | Apache MD5 (APR1) | `allow_legacy_hashes` | `htpasswd -nbm user pass` |
| `$1$` | md5crypt | `allow_legacy_hashes` | `openssl passwd -1 pass` |
| `{SHA}` | unsalted SHA-1 | `allow_legacy_hashes` | `htpasswd -nbs user pass` |

Notes:

- Plain text passwords and classic crypt(3) DES hashes are never accepted.
- Unknown usernames are checked against a dummy bcrypt hash so response time does not reveal which usernames exist.

::: warning Breaking change
APR1, md5crypt and `{SHA}` hashes were accepted by default before. They are fast to brute force when the configuration leaks. Replace them with bcrypt or argon2id, or set `allow_legacy_hashes: true` until you do.
:::
