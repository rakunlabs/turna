# block

`block` rejects matching HTTP methods or paths with `403 Forbidden`.

```yaml
server:
  http:
    middlewares:
      deny_writes:
        block:
          methods:
            - POST
            - PUT
            - DELETE
          regex_path: ^/admin/.*$
```

| Field | Description |
| --- | --- |
| `methods` | HTTP methods to block. Matching is case-insensitive after methods are uppercased. |
| `regex_path` | Optional Go regular expression. Matching paths are blocked. |

Requests that do not match continue to the next middleware.
