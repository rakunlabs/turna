# inject

`inject` rewrites response bodies after the next middleware returns. It matches requests by path using doublestar patterns.

```yaml
server:
  http:
    middlewares:
      inject_html:
        inject:
          path_map:
            "**/*.html":
              - old: "</head>"
                new: |
                  <script src="/runtime.js"></script>
                  </head>
```

## Replacement Fields

| Field | Description |
| --- | --- |
| `regex` | Regular expression to replace. When set, it overrides `old`. |
| `old` | Literal string to replace. |
| `new` | Replacement string. |
| `add_prefix` | String inserted at the beginning of the response body. |
| `add_postfix` | String appended to the response body. |
| `value` | Name of a loaded `map[string]any`. Each key is replaced with its value. |
| `delay` | Optional duration to sleep before applying this replacement. |

`inject` can read and rewrite gzip responses when the upstream response has `Content-Encoding: gzip`; it recompresses the body before returning it.
