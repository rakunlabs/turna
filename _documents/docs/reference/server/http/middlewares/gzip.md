# gzip

`gzip` compresses HTTP responses using chi's compression middleware.

```yaml
server:
  http:
    middlewares:
      compress:
        gzip:
          level: 5
```

| Field | Default | Description |
| --- | --- | --- |
| `level` | `5` | Compression level. |
