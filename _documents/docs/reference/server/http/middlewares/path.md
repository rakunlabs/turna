# path

`path` replaces the request path and optionally sets or deletes request headers.

```yaml
server:
  http:
    middlewares:
      force_health_path:
        path:
          path: /health
          headers:
            X-Original-Path: ""
            X-Rewrite: turna
```

| Field | Description |
| --- | --- |
| `path` | New value for `r.URL.Path`. |
| `headers` | Request headers to set. Empty values delete headers. |

Use [`regex_path`](./regex_path) or [`url`](./url) when the new path depends on the original request.
