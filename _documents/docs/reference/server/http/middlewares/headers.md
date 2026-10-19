# headers

`headers` sets, appends or deletes request and response headers.

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
            Server: ""
          append_response_headers:
            Vary: Origin
```

| Field | Description |
| --- | --- |
| `custom_request_headers` | Headers set on the request before the next middleware. |
| `custom_response_headers` | Headers set on the response. |
| `append_request_headers` | Values added to request headers, keeping existing values. |
| `append_response_headers` | Values added to response headers, keeping existing values. |

Set a header value to an empty string to delete that header.

Response headers are applied when the response starts, so they also replace or delete headers sent by the upstream, such as `Server`.
