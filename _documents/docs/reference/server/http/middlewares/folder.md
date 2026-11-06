# folder

`folder` serves files from a directory. It can also provide directory browsing, SPA fallback, path rewrites, and cache headers.

```yaml
server:
  http:
    middlewares:
      docs:
        folder:
          path: ./dist
          index: true
          spa: true
          browse: false
          cache_regex:
            - regex: .*
              cache_control: no-cache
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `base_path` | `/` | Base path used by the browse UI. |
| `path` | | Filesystem path to serve. |
| `index` | `false` | Serve `index_name` for directory requests when it exists. |
| `strip_index_name` | `false` | Redirect `/index.html` to `/`. |
| `index_name` | `index.html` | Index file name. |
| `spa` | `false` | Serve the SPA index for missing non-file paths. |
| `spa_enable_file` | `false` | Also serve the SPA index for missing paths that look like files. |
| `spa_index` | `index_name` | SPA fallback file. |
| `spa_index_regex` | | Regex replacements that choose the SPA index from the URL path. |
| `browse` | `false` | Enable directory listing. |
| `utc` | `false` | Render browse timestamps in UTC. |
| `prefix_path` | | Prefix stripped before filesystem lookup. |
| `file_path_regex` | | Regex replacements applied to the cleaned filesystem path. |
| `cache_regex` | | Regex-based `Cache-Control` rules, matched against the file name (`main.js`, not the path). The first match wins. |
| `browse_cache` | `no-cache` | `Cache-Control` value for browse pages. |
| `disable_folder_slash_redirect` | `false` | Disable automatic slash redirect for directories. |
| `etag` | | ETag generation for files: `weak` or `strong`. Empty disables it. |
| `precompressed` | | Serve precompressed sidecar files, encodings in order of preference: `br`, `zstd`, `gzip`. |

## ETag

When `etag` is set, served files get an `ETag` header and conditional requests (`If-None-Match`, `If-Match`, `If-Range`) are answered with `304 Not Modified` / `412` as appropriate.

| Mode | Value | Notes |
| --- | --- | --- |
| `weak` | `W/"<modtime>-<size>"` | Cheap, no file read. Skipped for filesystems without a modification time. |
| `strong` | `"<sha256 of content>"` | Content hash, cached per file path, modification time and size. Works for embedded filesystems. |

```yaml
folder:
  path: ./dist
  etag: strong
  cache_regex:
    - regex: index\.html$
      cache_control: no-cache
```

`Last-Modified` / `If-Modified-Since` keep working independently of this setting.

## Precompressed Files

With `precompressed`, a request for `app.js` is answered with `app.js.br`, `app.js.zst` or `app.js.gz` when that file exists and the client accepts the encoding in `Accept-Encoding`. Higher q-values win, `q=0` disables an encoding, and ties follow the configured order. Without an acceptable sidecar the original file is served.

```yaml
folder:
  path: ./dist
  spa: true
  etag: strong
  precompressed: [br, zstd, gzip]
```

- `Content-Encoding` is set and `Content-Type` comes from the original file name (`application/octet-stream` if unknown).
- `Vary: Accept-Encoding` is added to file responses whenever a sidecar was looked up, also when the original is served.
- ETags get the encoding as suffix, e.g. `"<hash>-br"`, so each representation has its own.
- Range requests apply to the compressed bytes.
- Index and SPA index files use sidecars too; files that are already `.br`, `.zst` or `.gz` are served as they are.
- The original file must exist, sidecars are not served for missing files.

The [`compress`](./compress) middleware leaves responses with `Content-Encoding` untouched. When `compress` runs before `folder` and picks an encoding, it removes `Accept-Encoding` from the request, so `folder` serves the original file and `compress` compresses it on the fly. Sidecars are then only used for `HEAD` and `Range` requests. Leave `compress` out of routes that serve precompressed files.

## Versioned Docs Example

```yaml
folder:
  path: ./site
  spa: true
  index: true
  spa_index_regex:
    - regex: ^/docs/([^/]*)/.*$
      replacement: /$1/index.html
  file_path_regex:
    - regex: ^/docs/([^/]*)/?(.*)$
      replacement: /$1/latest/$2
```
