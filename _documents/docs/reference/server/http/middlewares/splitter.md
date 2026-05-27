# splitter

`splitter` selects a middleware sub-chain by evaluating expressions against the request. The first rule that returns true wins.

```yaml
server:
  http:
    middlewares:
      v1:
        service:
          loadbalancer:
            servers:
              - url: http://v1:3000
      v2:
        service:
          loadbalancer:
            servers:
              - url: http://v2:3000
      version_splitter:
        splitter:
          rules:
            - rule: Header(`X-Version`, `v2`)
              middlewares:
                - v2
            - rule: "true"
              middlewares:
                - v1
```

## Expression Helpers

| Helper | Description |
| --- | --- |
| `Header(key, value)` | Header equals value. |
| `Path(pattern)` | URL path matches a doublestar pattern. |
| `PathPrefix(prefix)` | URL path has prefix. |
| `Method(method)` | HTTP method equals value, case-insensitive. |
| `Host(host)` | Host equals value. |
| `Query(key, value)` | Query parameter equals value. |

If no rule matches, Turna returns `404`.
