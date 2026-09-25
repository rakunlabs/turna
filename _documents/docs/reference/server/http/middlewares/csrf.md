# csrf

`csrf` protects state-changing requests against cross-site request forgery. It has two layers:

1. **Origin check** (enabled by default): browser requests with unsafe methods are rejected when `Sec-Fetch-Site` reports a cross-site request or when the `Origin` host does not match the `Host` header. It uses Go's `http.CrossOriginProtection` and needs no state or client changes.
2. **Token check** (optional): signed double-submit cookie. A token cookie is issued on safe requests and unsafe requests must send the same value in a header or form field.

`GET`, `HEAD`, `OPTIONS` (and `TRACE` for the token check) are always allowed; they must not change state. Requests without `Sec-Fetch-Site` and `Origin` headers (non-browser clients such as `curl`) pass the origin check.

Rejected requests get `403 Forbidden`.

```yaml
server:
  http:
    middlewares:
      csrf:
        csrf:
          trusted_origins:
            - https://admin.example.com
          bypass_patterns:
            - POST /webhook/
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `trusted_origins` | | Origins (`scheme://host[:port]`) allowed to send cross-origin requests. |
| `bypass_patterns` | | [ServeMux patterns](https://pkg.go.dev/net/http#hdr-Patterns) that skip all checks, e.g. `POST /webhook/{id}` or `/api/public/`. |
| `disable_origin_check` | `false` | Disable the origin check, for example when only the token check is wanted. |
| `token.enabled` | `false` | Enable the double-submit token check. |
| `token.secret` | | HMAC key to sign tokens. Recommended, it prevents subdomains from injecting a known token cookie. |
| `token.cookie_name` | `csrf_token` | Token cookie name. |
| `token.header_name` | `X-CSRF-Token` | Request header carrying the token. |
| `token.form_field` | `csrf_token` | Form field checked when the header is empty (url-encoded and multipart forms). |
| `token.cookie_path` | `/` | Cookie path. |
| `token.cookie_domain` | | Cookie domain. |
| `token.cookie_max_age` | session | Cookie lifetime, e.g. `12h`. |
| `token.cookie_secure` | `true` | Set the `Secure` flag. Disable only for plain HTTP development. |
| `token.cookie_same_site` | `lax` | `lax`, `strict` or `none` (`none` requires `cookie_secure`). |

## Token Example

```yaml
csrf:
  token:
    enabled: true
    secret: ${CSRF_SECRET}
```

The cookie is not `HttpOnly`, so the frontend can read it and send it back:

```js
const token = document.cookie
  .split('; ')
  .find((c) => c.startsWith('csrf_token='))
  ?.split('=')[1]

await fetch('/api/items', {
  method: 'POST',
  headers: { 'X-CSRF-Token': token },
  body: JSON.stringify(item),
})
```

For HTML forms add a hidden `csrf_token` field with the cookie value.

Place `csrf` before [`session`](./session) or any middleware that relies on cookies for authentication. Bearer-token APIs are not vulnerable to CSRF and can be listed in `bypass_patterns`.
