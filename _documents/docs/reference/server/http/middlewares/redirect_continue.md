# redirect_continue

`redirect_continue` tries regex redirect rules in order. If a rule changes the current path/query string, Turna redirects. If no rule changes it, the request continues to the next middleware.

```yaml
server:
  http:
    middlewares:
      normalize_api:
        redirect_continue:
          permanent: false
          redirects:
            - regex: ^/api/v1/(.*)$
              replacement: /api/$1
```

| Field | Default | Description |
| --- | --- | --- |
| `redirects` | required | Ordered regex replacement rules. |
| `redirects[].regex` | | Go regular expression matched against path plus query/fragment. |
| `redirects[].replacement` | | Redirect target generated with `ReplaceAllString`. |
| `permanent` | `false` | Use `308 Permanent Redirect` instead of `307 Temporary Redirect`. |
