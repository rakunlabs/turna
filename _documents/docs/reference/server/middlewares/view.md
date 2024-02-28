# View

Middleware to show API documentations in one place.

```yaml
middlewares:
  view:
    view:
      prefix_path: ""
      info_url: ""
      info_url_type: "yaml" # yaml or json
      insecure_skip_verify: false
      info:
        swagger: # swagger api list
          - name: ""
            link: ""
            schemes: []
            host: ""
            base_path: ""
            base_path_prefix: ""
            disable_authorize_button: false
        swagger_settings: # default settings for swagger
          schemes: []
          host: ""
          base_path: ""
          base_path_prefix: ""
          disable_authorize_button: false
```

> Check view example.
