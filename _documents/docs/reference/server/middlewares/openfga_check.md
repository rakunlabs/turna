# OpenFGA Check

This middleware is used to check if the user has the right to access the resource with OpenFGA's API.

In here, we get the preferred_username from the token and it will get the user_id from our custom openfga's middleware API. Then it will call API with method as resource and name of the api's service.

```yaml
server:
  http:
    middlewares:
      openfga_check:
        openfga_check:
          openfga_check_api: "http://localhost:8082/openfga/api/openfga/stores/<store_id>/check"
          openfga_user_api: "http://localhost:8082/openfga/api/user"
          openfga_model_id: <model_id>
          database:
            postgres: "postgres://postgres:password@localhost:5432/postgres?sslmode=disable&search_path=openfga"
          operation:
            parse:
              enable: true
              api_name_rgx: "^/([^/]*)/([^/]*)/?(.*)$"
              api_name_replacement: $2
              default_user_claim: "preferred_username"
              method:
                head: viewer
                options: viewer
                connect: viewer
                trace: viewer
                get: viewer
                post: editor
                put: editor
                patch: editor
                delete: editor
```

> Check the example of openfga_check.
