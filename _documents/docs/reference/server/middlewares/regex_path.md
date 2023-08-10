# regex_path

Use regex to replace URL path.

It uses golang's std regex ReplaceAllString function.

```yaml
middlewares:
  test:
    regex_path:
      regex: ^/v1/secret/data/(.*)$
      replacement: /v1/secret/kv2/data/$1
```
