# compress

`compress` compresses HTTP responses with `zstd`, `br` (Brotli) or `gzip`, chosen from the client's `Accept-Encoding` header.

```yaml
server:
  http:
    middlewares:
      compress:
        compress: {}
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `encodings` | `[zstd, br, gzip]` | Enabled encodings in server preference order. |
| `min_length` | `1024` | Responses smaller than this (bytes) are sent uncompressed. |
| `excluded_content_types` | see below | Content types that are never compressed. `type/*` wildcards are supported. |
| `included_content_types` | | When set, only these content types are compressed. |
| `gzip_level` | `-1` (default) | gzip level, `1`-`9`. |
| `brotli_level` | `4` | Brotli quality, `0`-`11`. Use higher values only for cached static content. |
| `zstd_level` | `3` | zstd level, `1`-`22`, mapped to the closest encoder speed. |

## Encoding selection

The encoding with the highest `q` value in `Accept-Encoding` wins. When several share the same quality, the order of `encodings` decides. `*` matches every enabled encoding and `q=0` disables one.

| Accept-Encoding | Result |
| --- | --- |
| `gzip, deflate, br, zstd` | `zstd` |
| `gzip, br` | `br` |
| `gzip;q=1, br;q=0.5` | `gzip` |
| `identity` | not compressed |

## Skipped responses

Responses are passed through unchanged when:

- the request is a `HEAD`, `Range` or WebSocket upgrade request,
- the status is `1xx`, `204`, `206` or `304`,
- the response already has `Content-Encoding` or `Content-Range`,
- `Cache-Control` contains `no-transform`,
- the content type is excluded,
- the body is smaller than `min_length`.

The default excluded types are already compressed formats: `image/*` (except `image/svg+xml`, `image/bmp` and icons), `video/*`, `audio/*`, `font/woff`, `font/woff2`, archive types, `application/octet-stream` and `application/grpc`. Setting `excluded_content_types` replaces this list.

## Notes

- `Vary: Accept-Encoding` is always added.
- A strong `ETag` is converted to a weak one when the body is compressed.
- `Accept-Encoding` is removed from the request before calling the next handler, so a proxied upstream sends an uncompressed body and turna compresses it once.
- Flushed (streaming / SSE) responses are compressed and flushed immediately.

Use [`decompress`](./decompress) to decode compressed request bodies.
