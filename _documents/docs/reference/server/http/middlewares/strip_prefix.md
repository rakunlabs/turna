# strip_prefix

`strip_prefix` removes one configured prefix from the request path.

```yaml
server:
  http:
    middlewares:
      strip_api:
        strip_prefix:
          prefix: /api
          force_slash: true
```

| Field | Default | Description |
| --- | --- | --- |
| `prefix` | | Single prefix to strip. |
| `prefixes` | | Multiple prefixes. When set, this takes priority over `prefix`. |
| `force_slash` | `true` | Ensure the result starts with `/`. |

If the request path does not start with a configured prefix, it continues unchanged except for `force_slash` normalization.
