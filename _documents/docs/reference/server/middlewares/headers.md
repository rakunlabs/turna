# headers

Custom response and request headers.

```yaml
middlewares:
  test:
    headers:
      custom_request_headers: # default is empty, map[string]string
        X-Request-Test: "123"
      custom_response_headers: # default is empty, map[string]string
        X-Response-Test: "123"
```
