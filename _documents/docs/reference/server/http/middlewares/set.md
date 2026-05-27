# set

`set` stores values in Turna's request context for later middlewares.

```yaml
server:
  http:
    middlewares:
      token_mode:
        set:
          values:
            - token_header
            - disable_redirect
          map:
            cookie_name: api_session
```

| Field | Description |
| --- | --- |
| `values` | Each listed key is stored with boolean value `true`. |
| `map` | Arbitrary key/value pairs stored in request context. |

Common session/login context keys include `token_header`, `token_header_delete`, `disable_redirect`, `cookie_name`, and `logout`.
