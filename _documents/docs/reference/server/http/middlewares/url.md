# url

`url` modifies URL components by ordered rules. It can change scheme, host, path, query, fragment, and port.

```yaml
server:
  http:
    middlewares:
      rewrite_url:
        url:
          rules:
            - path_match:
                - /api/**
              modify:
                strip_prefix: /api
                path_prefix: /v1
                add_query:
                  source: turna
            - always_match: true
              modify:
                scheme: http
```

## Match Fields

| Field | Description |
| --- | --- |
| `always_match` | Rule always matches. |
| `host_match` | Host doublestar patterns. |
| `path_match` | Path doublestar patterns. |

Avoid mixing `host_match` and `path_match` in the same rule; use separate ordered rules when both dimensions matter.

## Modify Fields

| Field | Description |
| --- | --- |
| `scheme` | Replace URL scheme. |
| `host` | Replace URL host and also update `r.Host`. |
| `port` | Replace the port when a host is set. |
| `path` | Replace the path completely and skip other path modifications. |
| `path_prefix` | Add a path prefix. |
| `path_suffix` | Add a path suffix. |
| `strip_prefix` | Remove a path prefix. |
| `path_replace` | Regex used for path replacement. |
| `path_replace_with` | Replacement for `path_replace`. |
| `add_query` | Query parameters to add. |
| `set_query` | Query parameters to set. |
| `remove_query` | Query parameter keys to remove. |
| `fragment` | Replace URL fragment. |

The first matching rule is applied, then the request continues to the next middleware.
