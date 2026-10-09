# block

`block` closes a share of connections and passes the others to the next middleware.

```yaml
server:
  tcp:
    middlewares:
      drop_tenth:
        block:
          percent: 10
```

| Field | Default | Description |
| --- | --- | --- |
| `percent` | `100` | Percentage of connections to close, 0-100. |
