# basic_auth

`basic_auth` protects a route with HTTP Basic authentication. Users are configured as `username:htpasswd_hash` entries and are checked with `github.com/abbot/go-http-auth`.

```yaml
server:
  http:
    middlewares:
      private_auth:
        basic_auth:
          realm: Restricted
          users:
            - "test:$apr1$JMWtQHoL$g/5ey5x7psJM7htuB6OEy0"
          header_field: X-User
          remove_header: true
```

| Field | Default | Description |
| --- | --- | --- |
| `users` | | List of `username:hash` credentials. |
| `realm` | `Restricted` | Realm sent in `WWW-Authenticate`. |
| `header_field` | `X-User` | Request header set to the authenticated username. Set an empty value to disable. |
| `remove_header` | `false` | Remove the original `Authorization` header after successful authentication. |
