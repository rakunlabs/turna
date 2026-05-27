# session_info

`session_info` returns selected claims from the access token stored by a [`session`](./session) middleware.

```yaml
server:
  http:
    middlewares:
      current_user:
        session_info:
          session_middleware: session
          information:
            values:
              - preferred_username
              - email
            custom:
              source: turna
            roles: true
            scopes: true
```

| Field | Description |
| --- | --- |
| `session_middleware` | Session middleware instance name. |
| `information.values` | Claim keys copied into the response. |
| `information.custom` | Extra static values added to the response. |
| `information.roles` | Include roles parsed from the token. |
| `information.scopes` | Include scopes parsed from the token. |

The route should usually run after the browser has already logged in, but it does not need to chain `session` in the same router because it reads the session store by middleware name.
