# block

`block` drops a share of datagrams and passes the others to the next middleware.

```yaml
server:
  udp:
    middlewares:
      drop_all:
        block: {}
```

| Field | Default | Description |
| --- | --- | --- |
| `percent` | `100` | Percentage of datagrams to drop, 0-100. |
