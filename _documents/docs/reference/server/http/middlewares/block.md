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
          regex_path:
            - ^/admin/.*$
            - \.env$
```

| Field | Default | Description |
| --- | --- | --- |
| `methods` | | HTTP methods to block. Matching is case-insensitive after methods are uppercased. |
| `regex_path` | | Go regular expression, or a list of them. Paths matching any are blocked. |
| `status_code` | `403` | Status of blocked requests, e.g. `404` to hide the path. |
| `message` | | Error message of the response body. |

Requests that do not match continue to the next middleware. To block by client IP, use [`ip_filter`](./ip_filter).
