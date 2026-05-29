# gzip

`gzip` compresses HTTP responses using `github.com/rakunlabs/ada/middleware/encoding`.

```yaml
server:
  http:
    middlewares:
      compress:
        gzip: {}
```

| Field | Default | Description |
| --- | --- | --- |
| `level` | `5` | Deprecated/no-op. Kept for backward compatibility; the encoding middleware always uses the default gzip level. |
