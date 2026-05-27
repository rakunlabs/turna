# info

`info` returns the value of a cookie or a value stored inside a configured `session` middleware.

```yaml
server:
  http:
    middlewares:
      token_info:
        info:
          cookie: auth_session
          session: true
          session_middleware: session
          session_value_name: token
          base64: true
          raw: false
```

| Field | Default | Description |
| --- | --- | --- |
| `cookie` | | Cookie name to read, or session cookie name when `session` is true. |
| `session` | `false` | Read from the named session store instead of directly from a cookie. |
| `session_middleware` | `session` | Session middleware instance name. |
| `session_value_name` | `token` | Session value key to return. |
| `base64` | `false` | Base64-decode the returned value. |
| `raw` | `false` | Return `text/plain` instead of JSON. |

When `session` is true, the session middleware must be registered before `info` initializes.
