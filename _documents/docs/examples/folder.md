# File Server

This example serves the current directory with directory browsing enabled.

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      files:
        folder:
          path: ./
          browse: true
          spa: false
          index: false
          cache_regex:
            - regex: .*
              cache_control: no-cache
    routers:
      files:
        path: /*
        middlewares:
          - files
```

For a frontend SPA, set `path: ./dist`, `index: true`, and `spa: true`.
