# iam_forward_auth

`iam_forward_auth` exposes a forward-auth style endpoint for external proxies. It reads the original request details from forwarded headers and returns `200` when IAM allows access.

```yaml
server:
  http:
    middlewares:
      forward_auth:
        iam_forward_auth:
          iam_middleware: iam
          check_api: ""
          method_header: X-Forwarded-Method
          host_header: X-Forwarded-Host
          uri_header: X-Forwarded-Uri
          pass_headers:
            - X-User
          insecure_skip_verify: false
```

## Modes

Use one of these modes:

| Mode | Configuration |
| --- | --- |
| In-process | Set `iam_middleware` to a registered [`iam`](./iam) middleware name. |
| HTTP | Leave `iam_middleware` empty and set `check_api`. |

Required request headers default to `X-Forwarded-Method`, `X-Forwarded-Host`, and `X-Forwarded-Uri`. `X-User` must also be present.

`pass_headers` copies selected request headers to the response on successful authorization.
