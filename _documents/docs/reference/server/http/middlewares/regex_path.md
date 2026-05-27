# regex_path

`regex_path` rewrites `r.URL.Path` with Go's `regexp.ReplaceAllString`.

```yaml
server:
  http:
    middlewares:
      kv2_path:
        regex_path:
          regex: ^/v1/secret/data/(.*)$
          replacement: /v1/secret/kv2/data/$1
```

The rewritten path is passed to the next middleware in the chain.
