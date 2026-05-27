# print

`print` writes POST request bodies to stderr and returns `204 No Content`. It is intended for debugging webhooks and callbacks.

```yaml
server:
  http:
    middlewares:
      debug_body:
        print:
          text: "\n--- end body ---"
```

Behavior:

- `GET` requests continue to the next middleware.
- `POST` bodies are printed to stderr and the chain stops with `204`.
- Other methods return `405 Method Not Allowed`.
