# bandwidth_limit

`bandwidth_limit` limits the transfer rate of each connection.

```yaml
server:
  tcp:
    middlewares:
      throttle:
        bandwidth_limit:
          upload: 1048576   # 1 MiB/s from the client
          download: 5242880 # 5 MiB/s to the client
```

| Field | Default | Description |
| --- | --- | --- |
| `upload` | `0` | Bytes per second read from the client, `0` is unlimited. |
| `download` | `0` | Bytes per second written to the client, `0` is unlimited. |
| `burst` | rate | Bytes allowed at once, defaults to one second of traffic. |

At least one of `upload` or `download` is required. The limit applies per connection.
