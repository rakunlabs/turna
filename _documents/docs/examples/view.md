# View

Swagger pages in one place.

```yaml
server:
  entrypoints:
    web:
      address: ":8082"
  http:
    middlewares:
      info:
        hello:
          message: |
            swagger_settings:
              base_path_prefix: /api
              disable_authorize_button: true
              schemes: ["HTTPS"]
            swagger:
              - name: test1
                link: https://petstore.swagger.io/v2/swagger.json
                base_path_prefix: /api
                disable_authorize_button: true
              - name: test2
                link: https://petstore.swagger.io/v2/swagger.json
                disable_authorize_button: false
              - name: test3
                link: https://petstore.swagger.io/v2/swagger.json
      view:
        view:
          prefix_path: /view/
          # info_url: http://localhost:8082/info
          info_url_type: YAML
          info:
            swagger_settings:
              base_path_prefix: /api
              disable_authorize_button: true
              schemes: ["HTTPS"]
            swagger:
              - name: test1
                link: https://petstore.swagger.io/v2/swagger.json
                base_path_prefix: /api
                disable_authorize_button: true
              - name: test2
                link: https://petstore.swagger.io/v2/swagger.json
                disable_authorize_button: false
              - name: test3
                link: https://petstore.swagger.io/v2/swagger.json
    routers:
      view:
        path: /view/*
        middlewares:
          - view
      info:
        path: /info
        middlewares:
          - info
```
