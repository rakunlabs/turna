# request

`request` makes an outbound HTTP request and returns that response to the client. It does not call the next middleware.

```yaml
server:
  http:
    middlewares:
      brewery_api:
        request:
          url_rgx: ^/breweries/(.*)$
          url: https://api.openbrewerydb.org/v1/breweries/$1
          method: GET
          body: ""
          headers:
            Accept: application/json
          insecure_skip_verify: false
          enable_retry: false
```

| Field | Description |
| --- | --- |
| `url` | Target URL. If `url_rgx` is set, this is the replacement string. |
| `url_rgx` | Optional regex applied to the incoming path to produce the target URL. |
| `method` | Outbound method. |
| `body` | Static outbound body. |
| `headers` | Outbound request headers. |
| `insecure_skip_verify` | Skip upstream TLS verification. |
| `enable_retry` | Enable klient retry behavior. |

Response status, headers, and body from the outbound request are copied back to the client.
