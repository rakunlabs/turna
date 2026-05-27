# redirection

`redirection` always redirects and does not call the next middleware.

```yaml
server:
  http:
    middlewares:
      to_docs:
        redirection:
          url: /docs/
          permanent: true
```

| Field | Default | Description |
| --- | --- | --- |
| `url` | `/` | Redirect target. |
| `permanent` | `false` | Use `308 Permanent Redirect` instead of `307 Temporary Redirect`. |
