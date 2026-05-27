# iam_check

`iam_check` authorizes a request by calling an IAM check API. It expects an authenticated user in the `X-User` request header.

```yaml
server:
  http:
    middlewares:
      permissions:
        iam_check:
          check_api: http://localhost:8080/iam/v1/check
          force_host: ""
          insecure_skip_verify: false
          public:
            - paths:
                - /health
              methods:
                - GET
          responses:
            - path: /admin/**
              methods:
                - GET
              message: admin access required
```

| Field | Description |
| --- | --- |
| `check_api` | IAM endpoint that accepts `{alias,path,method,host}` and returns `{allowed}`. |
| `public` | Resources that bypass the IAM check. Host/path matching uses doublestar. |
| `responses` | Custom forbidden responses or redirects for denied requests. |
| `force_host` | Host value sent to IAM instead of `r.Host`. |
| `insecure_skip_verify` | Skip TLS verification for the IAM API request. |

When the request is not public and `X-User` is missing, the middleware returns `401`. When IAM denies access, it returns `403` unless a matching custom response redirects.
