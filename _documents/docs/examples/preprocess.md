# PreProcess

Replace all values in a file with the values from a static before serving it.

```yaml
loads:
  - statics:
    - content:
        content: |
          Turna: XXX2
        name: values

preprocess:
  - replace:
      path: ./testdata/html
      contents:
        value: values

server:
  entrypoints:
    web:
      address: ":8082"
  http:
    middlewares:
      project:
        folder:
          path: ./testdata/html
          browse: false
          spa: false
          index: true
          cache_regex:
            - regex: .*
              cache_control: no-cache
    routers:
      project:
        path: /*
        middlewares:
          - project
```
