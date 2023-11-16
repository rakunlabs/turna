# File Server

Simple browser file server.

```yaml
server:
  entrypoints:
    http:
      address: ":8080"
  http:
    middlewares:
      project:
        folder:
          path: ./
          browse: true
          spa: false
          index: false
          cache_regex:
            - regex: .*
              cache_control: no-cache
    routers:
      project:
        path: /*
        middlewares:
          - project
```
