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
| `cache_regex` | | Regex-based `Cache-Control` rules. |
| `browse_cache` | `no-cache` | `Cache-Control` value for browse pages. |
| `disable_folder_slash_redirect` | `false` | Disable automatic slash redirect for directories. |

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
