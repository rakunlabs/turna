# body_limit

`body_limit` rejects request bodies larger than a configured size with `413 Request Entity Too Large`.

```yaml
server:
  http:
    middlewares:
      upload_limit:
        body_limit:
          max_size: 10MB
```

| Field | Description |
| --- | --- |
| `max_size` | Required. Maximum body size, e.g. `512KB`, `10MiB`, `1GB` or bytes like `1048576`. |

Requests with a larger `Content-Length` are rejected before the next middleware runs. Bodies without a length (chunked) are cut at the limit: reading past it fails, and `service` answers with an error.

Put `body_limit` after `decompress` to limit the decompressed size.
