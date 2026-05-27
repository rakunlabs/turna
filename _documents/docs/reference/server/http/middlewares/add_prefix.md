# add_prefix

`add_prefix` prepends a path segment to `r.URL.Path` before the next middleware runs.

```yaml
server:
  http:
    middlewares:
      add_api:
        add_prefix:
          prefix: /api
```

If the incoming path is `/users`, the next middleware sees `/api/users`.

Use `strip_prefix` for the opposite operation.
