# view

`view` serves a combined API documentation UI for Swagger definitions.

```yaml
server:
  http:
    middlewares:
      docs_view:
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
```

| Field | Description |
| --- | --- |
| `prefix_path` | Base path for the UI. |
| `info_url` | Optional URL that returns the Swagger list. |
| `info_url_type` | `yaml` or `json` when `info_url` is used. |
| `insecure_skip_verify` | Skip TLS verification while fetching `info_url`. |
| `info.swagger` | List of Swagger documents. |
| `info.swagger_settings` | Defaults applied to Swagger documents. |

Use `info_url` when the list should come from another service; otherwise configure `info` inline.
