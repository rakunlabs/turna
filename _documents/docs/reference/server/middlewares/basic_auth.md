# basic_auth

Basic authentication middleware.

For basic auth check uses `github.com/abbot/go-http-auth` package.  
Users as `htpasswd`.

```yaml
middlewares:
  test:
    basic_auth:
      users:
        - "test:$apr1$JMWtQHoL$g/5ey5x7psJM7htuB6OEy0" # pass
        - "test2:$apr1$u4NQ6Doq$KdCzBPfjarcQ0mk4Fd/3v1" # pass
      header_field: "" # default is empty, string, add the username to the request's header
      remove_header: false # default is false, bool, remove the Authorization header
```

