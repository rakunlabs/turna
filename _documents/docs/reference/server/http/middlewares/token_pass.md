# token_pass

`token_pass` renders a JWT payload, signs it, and either redirects to a URL containing the token or calls that URL and returns its response.

```yaml
server:
  http:
    middlewares:
      signed_dashboard:
        token_pass:
          secret_key: test_secret_key
          signing_method: HS256
          default_exp_duration: 10m
          payload: |
            resource:
              dashboard: 1
            params:
              user: {{ index .headers "X-User" }}
          redirect_url: http://dashboard.local/embed/{{ .token }}
          redirect_with_code: true
          method: GET
          enable_body: false
          body_raw: false
          headers: {}
```

## Fields

| Field | Description |
| --- | --- |
| `secret_key` | HMAC signing key. |
| `signing_method` | JWT signing method. Defaults to `HS256` when invalid or empty. |
| `payload` | YAML claims rendered as a template. |
| `default_exp_duration` | Duration used to add `exp` when the payload omits it. Set a valid duration such as `10m` or `0s`. |
| `redirect_url` | Template rendered after `.token` is added to template data. |
| `redirect_with_code` | Redirect to `redirect_url` instead of making a backend request. |
| `method` | Backend request method. Defaults to `GET`. |
| `enable_body` | Send a request body when not redirecting. |
| `body_raw` | Reuse the incoming request body when `enable_body` is true. |
| `body` | Template for backend request body when `body_raw` is false. |
| `additional_values` | Extra values exposed as `.values`. |
| `insecure_skip_verify` | Skip TLS verification for backend request. |
| `enable_retry` | Enable klient retry behavior. |
| `headers` | Backend request headers. |
| `debug_token` | Log generated token at debug level. |
| `debug_payload` | Log rendered payload at debug level. |

Template data includes `body`, `body_raw`, `method`, `headers`, `query_params`, `cookies`, `path`, and `values`.
