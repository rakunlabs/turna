# Preprocess

This example replaces placeholders in `./testdata/html` with values loaded from configuration before serving the folder.

```yaml
loads:
  - name: frontend_env
    statics:
      - content:
          name: frontend_env
          content: |
            __APP_NAME__: Turna
            __API_URL__: http://localhost:8082/api

preprocess:
  - replace:
      path: ./testdata/html
      contents:
        - value: frontend_env

server:
  entrypoints:
    web:
      address: ":8082"
  http:
    middlewares:
      frontend:
        folder:
          path: ./testdata/html
          browse: false
          spa: false
          index: true
          cache_regex:
            - regex: .*
              cache_control: no-cache
    routers:
      frontend:
        path: /*
        middlewares:
          - frontend
```
