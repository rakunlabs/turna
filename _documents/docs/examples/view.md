# View

This example serves the API documentation UI under `/view/*` with inline Swagger entries.

```yaml
server:
  entrypoints:
    web:
      address: ":8082"
  http:
    middlewares:
      view:
        view:
          prefix_path: /view/
          info_url_type: yaml
          info:
            swagger_settings:
              base_path_prefix: /api
              disable_authorize_button: true
              schemes: [HTTPS]
            swagger:
              - name: petstore
                link: https://petstore.swagger.io/v2/swagger.json
                base_path_prefix: /api
                disable_authorize_button: true
              - name: petstore-public
                link: https://petstore.swagger.io/v2/swagger.json
    routers:
      view:
        path: /view/*
        middlewares:
          - view
```

Use `info_url` when the Swagger list should be fetched from another endpoint instead of being embedded in the Turna config.
