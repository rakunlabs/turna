# gzip

::: warning Deprecated
`gzip` is deprecated, use [`compress`](./compress). It supports gzip as well as Brotli and zstd, content type filters and a minimum size. `compress: {encodings: [gzip]}` behaves like `gzip`. Turna logs a warning when `gzip` is used.
:::

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
