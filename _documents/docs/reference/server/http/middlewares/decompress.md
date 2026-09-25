# decompress

`decompress` replaces a compressed request body with an uncompressed reader. Supported `Content-Encoding` values are `gzip` (`x-gzip`), `br` and `zstd`.

```yaml
server:
  http:
    middlewares:
      decompress_body:
        decompress: {}
```

After decoding, the `Content-Encoding` and `Content-Length` request headers are removed. Requests without an encoding or with an unknown encoding continue unchanged. A body that cannot be decoded returns `400 Bad Request`.
