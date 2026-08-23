# iam_check

`iam_check` authorizes a request by calling an IAM check API. Authenticated requests carry the user from `X-User`; anonymous requests are also sent to check APIs that support centrally managed public permissions (the `auth` middleware does).

```yaml
server:
  http:
    middlewares:
      permissions:
        iam_check:
          auth_middleware: auth
          # For a remote identity service instead:
          # check_api: https://identity.example.com/auth/check
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
| `auth_middleware` | Name of an auth middleware in the same Turna process. Performs the check directly without an HTTP self-call. Takes precedence over `check_api`. |
| `check_api` | Remote IAM/auth endpoint that accepts `{alias,path,method,host}` and returns `{allowed}`. Required when `auth_middleware` is empty. |
| `public` | Local resources that bypass the IAM check. Host/path matching uses doublestar. Prefer auth permission `public: true` when the rule should be managed centrally. |
| `responses` | Custom forbidden responses or redirects for denied requests. |
| `force_host` | Host value sent to IAM instead of `r.Host`. |
| `insecure_skip_verify` | Skip TLS verification for the IAM API request. |

When `X-User` is missing, `iam_check` asks the check API whether the host/path/method is public. A public match passes; a denial returns `401`. Check APIs from older IAM versions that reject identity-less checks also fall back to `401`. An authenticated denial returns `403` unless a matching custom response redirects.

When a [`session`](./session) middleware in front lists the auth in `auth_skip_paths`, session already runs the anonymous public check itself. On a match it sets the `public_access` context flag and `iam_check` passes the request without repeating the check — a `[session, iam_check]` chain costs one public check, not two. Plain session `skip_paths` matches do **not** set the flag: those requests still go through the full `iam_check` decision.

For auth in the same process, prefer the middleware name so a wrong prefix, a fronting session middleware, or a recursive route cannot break checks:

```yaml
auth_middleware: auth
```

For a remote auth middleware, point `check_api` at the non-admin X-User plane; `iam_check` forwards the identity header:

```yaml
check_api: http://localhost:8080/auth/check
```

`/auth/v1/check` remains the admin plane for checking an explicitly supplied alias/id; `/auth/check` uses forwarded `X-User`, or public permissions when the header is absent.

Remote non-200 responses are reported as `502 Bad Gateway` with the configured endpoint and upstream status (for example `check API "..." returned 404 Not Found`), rather than the old ambiguous `500 Not Found`.
