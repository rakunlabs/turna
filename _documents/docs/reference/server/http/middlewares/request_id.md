# request_id

`request_id` ensures every request has an `X-Request-Id` header. If the request does not provide one, Turna generates a ULID.

```yaml
server:
  http:
    middlewares:
      request_id_response:
        request_id:
          request_id_response: true
```

| Field | Default | Description |
| --- | --- | --- |
| `request_id_response` | `false` | Also set `X-Request-Id` on the response. |

HTTP routers already include request ID handling by default through `pre_middlewares.request_id`. Use this middleware when you need explicit response-header behavior or a manual position in the chain.
