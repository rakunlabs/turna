# try

`try` captures the downstream response. If the response status matches and the path can be rewritten, it reruns the downstream chain once with the rewritten path.

```yaml
server:
  http:
    middlewares:
      try_fallback:
        try:
          regex: ^/v1/(.*)$
          replacement: /v2/$1
          status_codes: "404,500-502"
```

| Field | Description |
| --- | --- |
| `regex` | Go regex applied to the original path. |
| `replacement` | Replacement path. |
| `status_codes` | Comma or space separated list. Ranges such as `500-505` are supported. |

If the replacement does not change the path, or the captured status is not in `status_codes`, the original response is returned.
