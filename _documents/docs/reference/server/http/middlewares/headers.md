# headers

`headers` sets or deletes request and response headers.

```yaml
server:
  http:
    middlewares:
      proxy_headers:
        headers:
          custom_request_headers:
            X-Forwarded-Prefix: /api
          custom_response_headers:
            X-Frame-Options: DENY
```

| Field | Description |
| --- | --- |
| `custom_request_headers` | Headers applied to the request before the next middleware. |
| `custom_response_headers` | Headers applied to the response before the next middleware. |

Set a header value to an empty string to delete that header.
