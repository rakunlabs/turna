# strip_prefix

Prefix middleware is use to strip prefix from the request path.

```yaml
middlewares:
  test:
    strip_prefix:
      force_slash: true # default is true, auto add slash begin of the path if not exist
      prefix: /test # default is empty
      prefixes: # default is empty, prefixes has priority over prefix
        - /test
        - /test2
```
