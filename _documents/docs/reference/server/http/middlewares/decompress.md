# decompress

`decompress` replaces a gzip request body with an uncompressed reader when `Content-Encoding: gzip` is present.

```yaml
server:
  http:
    middlewares:
      gunzip:
        decompress: {}
```

Requests without gzip encoding continue unchanged.
