# template

`template` captures the response body from the next middleware and replaces it with a rendered template.

```yaml
server:
  http:
    middlewares:
      html_template:
        template:
          raw_body: true
          headers:
            Content-Type: text/html; charset=utf-8
          template: |
            <html><body>{{ .body_raw | codec.ByteToString }}</body></html>
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `template` | | Template content to render. |
| `raw_body` | `false` | When false, parse the captured body as JSON before templating. When true, expose raw bytes. |
| `status_code` | captured status | Override the response status. |
| `value` | | Loaded data key expected to be a map. Exposed as `.value`. |
| `additional` | | Extra values exposed as `.additional`. |
| `headers` | | Response headers applied when the template is used. |
| `apply_status_codes` | all | Render only for selected captured response statuses. |
| `trust` | `false` | Enable powerful template functions. |
| `work_dir` | | Working directory for template functions that need one. |
| `delims` | `['{{', '}}']` | Two custom template delimiters. |

Template data includes `body`, `body_raw`, `method`, `headers`, `query_params`, `cookies`, `path`, `value`, and `additional`.
