# hello

`hello` returns a static or templated response and does not call the next middleware. It is useful for health checks, mock endpoints, and simple HTML pages.

```yaml
server:
  http:
    middlewares:
      health:
        hello:
          message: OK
          status_code: 200
          content_type: text/plain; charset=utf-8
          headers:
            Cache-Control: no-store
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `message` | `OK` | Response body or template. |
| `status_code` | `200` | Response status. |
| `headers` | | Response headers. |
| `content_type` | `text/plain; charset=utf-8` | Response content type. |
| `template` | `false` | Render `message` as a template. |
| `trust` | `false` | Enable powerful template functions. |
| `work_dir` | | Working directory for template functions that need one. |
| `delims` | `['{{', '}}']` | Two custom template delimiters. |

Template data includes `body`, `method`, `headers`, `query_params`, `cookies`, `path`, `host`, `scheme`, and `remote_addr`.
